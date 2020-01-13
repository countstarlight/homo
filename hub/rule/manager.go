package rule

import (
	"fmt"
	"github.com/countstarlight/homo/hub/common"
	"github.com/countstarlight/homo/hub/config"
	"github.com/countstarlight/homo/hub/router"
	"github.com/countstarlight/homo/utils"
	cmap "github.com/orcaman/concurrent-map"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

const (
	initial = int32(0)
	started = int32(1)
	closed  = int32(2)
)

var errRuleManagerClosed = fmt.Errorf("rule manager already closed")

// Report reports stats
type report func(map[string]interface{}) error

// Manager manages all rules of message routing
type Manager struct {
	status int32
	broker broker
	report report
	trieq0 *router.Trie
	rules  cmap.ConcurrentMap
	tomb   utils.Tomb
	log    *zap.SugaredLogger
}

// NewManager creates a new rule manager
func NewManager(c []config.Subscription, b broker, r report, log *zap.SugaredLogger) (*Manager, error) {
	m := &Manager{
		broker: b,
		report: r,
		rules:  cmap.New(),
		trieq0: router.NewTrie(),
		log:    log.With("manager", "rule"),
	}
	m.rules.Set(common.RuleMsgQ0, newRuleQos0(m.broker, m.trieq0, m.log))
	m.rules.Set(common.RuleTopic, newRuleTopic(m.broker, m.trieq0, m.log))
	for _, sub := range c {
		err := m.AddSinkSub(common.RuleTopic, sub.Target.Topic, uint32(sub.Source.QOS), sub.Source.Topic, uint32(sub.Target.QOS), sub.Target.Topic)
		if err != nil {
			return nil, fmt.Errorf("failed to add subscription (%v): %s", sub.Source, err.Error())
		}
	}
	if r != nil {
		return m, m.tomb.Go(m.reporting)
	}
	return m, nil
}

// Start starts all rules
func (m *Manager) Start() {
	if !atomic.CompareAndSwapInt32(&m.status, initial, started) {
		return
	}
	for item := range m.rules.IterBuffered() {
		r := item.Val.(base)
		// r.log.Info("To start rule")
		if err := r.start(); err != nil {
			m.log.Infow(fmt.Sprintf("failed to start rule (%s)", r.uid()), zap.Error(err))
		}
	}
}

// Close closes this manager
func (m *Manager) Close() {
	if !atomic.CompareAndSwapInt32(&m.status, started, closed) {
		return
	}
	m.log.Infof("rule manager closing")
	defer m.log.Infof("rule manager closed, remaining offsets: %d", m.broker.OffsetChanLen())
	m.tomb.Kill(nil)
	defer m.tomb.Wait()
	for item := range m.rules.IterBuffered() {
		r := item.Val.(base)
		// r.log.Info("To stop rule")
		r.stop()
	}
	// Wait all sinked messages are handled
	// TODO: how to handle the case of session closed by client during waiting
	for item := range m.rules.IterBuffered() {
		r := item.Val.(base)
		r.wait(false)
	}
	// wait all offsets persisted
	m.broker.WaitOffsetPersisted()
}

// StartRule starts a rule
func (m *Manager) StartRule(id string) error {
	if atomic.LoadInt32(&m.status) == closed {
		return errRuleManagerClosed
	} else if atomic.LoadInt32(&m.status) == initial {
		return nil
	}
	item, ok := m.rules.Get(id)
	if !ok {
		return fmt.Errorf("rule (%s) not found", id)
	}
	r := item.(base)
	// r.log.Info("To start rule")
	return r.start()
}

// RemoveRule removes a sink for session
func (m *Manager) RemoveRule(id string) error {
	if atomic.LoadInt32(&m.status) == closed {
		return errRuleManagerClosed
	}
	if item, ok := m.rules.Get(id); ok {
		m.rules.Remove(id)
		r := item.(base)
		// r.log.Info("To stop rule")
		r.stop()
		r.wait(true)
	}
	return nil
}

// AddRuleSess adds a new rule for session during running
func (m *Manager) AddRuleSess(id string, persistent bool, publish, republish common.Publish) error {
	if atomic.LoadInt32(&m.status) == closed {
		return errRuleManagerClosed
	}
	if _, ok := m.rules.Get(id); ok {
		return fmt.Errorf("rule (%s) exists", id)
	}
	m.rules.Set(id, newRuleSess(id, persistent, m.broker, m.trieq0, publish, republish, m.log))
	return nil
}

// AddSinkSub adds a sink subscription
func (m *Manager) AddSinkSub(ruleid, subid string, subqos uint32, subtopic string, pubqos uint32, pubtopic string) error {
	if atomic.LoadInt32(&m.status) == closed {
		return errRuleManagerClosed
	}
	item, ok := m.rules.Get(ruleid)
	if !ok {
		return fmt.Errorf("rule (%s) not found", ruleid)
	}
	r := item.(base)
	r.register(newSinkSub(subid, subqos, subtopic, pubqos, pubtopic, r.channel()))
	return nil
}

// RemoveSinkSub removes a sink subscription
func (m *Manager) RemoveSinkSub(id, topic string) error {
	if atomic.LoadInt32(&m.status) == closed {
		return errRuleManagerClosed
	}
	item, ok := m.rules.Get(id)
	if !ok {
		return fmt.Errorf("rule (%s) not found", id)
	}
	item.(base).remove(id, topic)
	return nil
}

func (m *Manager) reporting() error {
	defer m.log.Debugf("status logging task stopped")

	var err error
	t := time.NewTicker(m.broker.Config().Metrics.Report.Interval)
	defer t.Stop()
	for {
		select {
		case <-m.tomb.Dying():
			return nil
		case <-t.C:
			ruleStats := map[string]interface{}{}
			for item := range m.rules.IterBuffered() {
				r := item.Val.(base)
				ruleStats[r.uid()] = r.info()
			}
			stats := map[string]interface{}{
				"rule_count": len(ruleStats),
				"rule_stats": ruleStats,
			}
			err = m.report(stats)
			if err != nil {
				m.log.Warnf("failed to report rule stats")
			}
			m.log.Debug(stats)
		}
	}
}
