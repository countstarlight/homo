//
// Copyright (c) 2019-present Codist <countstarlight@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.
// Written by Codist <countstarlight@gmail.com>, March 2019
//

package nlu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/countstarlight/homo/cmd/webview/config"
	"github.com/countstarlight/homo/module/com"
	"io/ioutil"
	"net/http"
	"sort"
)

var intentsName = map[string]string{
	"confirm":        "表示确定",
	"ask_name":       "询问名字",
	"deny":           "表示拒绝",
	"goodbye":        "表示道别",
	"greet":          "表达问候",
	"inform_time":    "询问时间",
	"medical":        "咨询医药",
	"switch_mode":    "切换模式",
	"thanks":         "表达感谢",
	"request_search": "请求搜索",
}

var intentList []string

func init() {
	intentList = make([]string, 0, len(intentsName))
	for k := range intentsName {
		intentList = append(intentList, k)
	}
}

type IntentRankingList []struct {
	Name       string  `json:"name"`
	Confidence float64 `json:"confidence"`
}

type nluReply struct {
	Intent struct {
		Name       string  `json:"name"`
		Confidence float64 `json:"confidence"`
	} `json:"intent"`
	Entities      []interface{}     `json:"entities"`
	IntentRanking IntentRankingList `json:"intent_ranking"`
	Text          string            `json:"text"`
	Project       string            `json:"project"`
	Model         string            `json:"model"`
}

type intentRequest struct {
	Query   string `json:"q"`
	Project string `json:"project"`
	Model   string `json:"model"`
}

func (l IntentRankingList) Len() int {
	return len(l)
}

func (l IntentRankingList) Less(i, j int) bool {
	return l[i].Confidence > l[j].Confidence
}

func (l IntentRankingList) Swap(i, j int) {
	l[i],
		l[j] = l[j],
		l[i]
}

func ActionLocal(text string) ([]string, error) {
	postM := &intentRequest{
		Query:   text,
		Project: config.NluProject,
		Model:   config.NluModel,
	}
	var postJson, err = json.Marshal(postM)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", config.ParseAPI, bytes.NewBuffer(postJson))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer com.IOClose("", resp.Body)
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	reply := nluReply{}
	err = json.Unmarshal(body, &reply)
	if err != nil {
		return nil, err
	}
	if !com.IfStringInArray(reply.Intent.Name, actionList) {
		return nil, fmt.Errorf("意图[%s]没有对应的行为", reply.Intent.Name)
	}
	var (
		replyMessage []string
		result       string
		entitiesList map[string]string
	)
	if config.AnalyticalMode {
		//1.Get intent rank
		sort.Sort(reply.IntentRanking)
		rankList := reply.IntentRanking[:3]
		result = "意图分析: "
		for _, r := range rankList {
			if !com.IfStringInArray(r.Name, intentList) {
				result = result + fmt.Sprintf("[%s]: %.4f%% ", "未知", r.Confidence*100)
			} else {
				result = result + fmt.Sprintf("[%s]: %.4f%% ", intentsName[r.Name], r.Confidence*100)
			}
		}
		replyMessage = append(replyMessage, result)
	}
	//2.Get entities
	entitiesList = make(map[string]string)
	result = "实体分析: "
	if len(reply.Entities) > 0 {
		for _, e := range reply.Entities {
			v, ok := e.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("获取实体失败")
			}
			entitiesList[v["entity"].(string)] = v["value"].(string)
			result = result + fmt.Sprintf("[%s]: %s ", entitiesName[v["entity"].(string)], v["value"].(string))
		}
	} else {
		result = result + "无实体"
	}
	if config.AnalyticalMode {
		replyMessage = append(replyMessage, result)
	}

	//3.Run action
	result, err = RunActions[reply.Intent.Name](entitiesList)
	if err != nil {
		return nil, err
	}
	replyMessage = append(replyMessage, result)
	return replyMessage, err
}
