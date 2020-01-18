package engine

import (
	"fmt"
	"io"
	"strings"

	"github.com/countstarlight/homo/sdk/homo-go"
)

// all status
const (
	KeyName       = "name"
	KeyStatus     = "status"
	KeyCreateTime = "create_time"
	KeyStartTime  = "start_time"
	KeyFinishTime = "finish_time"

	// Created    = "created"    // 已创建
	Running = "running" // 运行中
	// Paused     = "paused"     // 已暂停
	Restarting = "restarting" // 重启中
	// Removing   = "removing"   // 退出中
	// Exited     = "exited"     // 已退出
	Dead = "dead" // 未启动（默认值）
	// Offline    = "offline"    // 离线（同核心的状态）
)

// Instance interfaces of instance
type Instance interface {
	Service() Service
	Name() string
	Info() PartialStats
	Stats() PartialStats
	Wait(w chan<- error)
	Dying() <-chan struct{}
	Restart() error
	Stop()
	io.Closer
}

// GenerateInstanceEnv generates new env of the instance
func GenerateInstanceEnv(name string, static []string, dynamic map[string]string) []string {
	var env []string
	dyn := dynamic != nil
	for _, v := range static {
		// remove auth token info for dynamic instances
		if dyn {
			if strings.HasPrefix(v, homo.EnvKeyServiceToken) {
				continue
			}
		}
		env = append(env, v)
	}
	env = append(env, fmt.Sprintf("%s=%s", homo.EnvKeyServiceInstanceName, name))
	if dyn {
		for k, v := range dynamic {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return env
}