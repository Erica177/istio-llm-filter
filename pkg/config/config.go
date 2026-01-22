// Copyright The AIGW Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package config 定义了 LLM Proxy Filter 的配置结构
package config

import (
	"errors"
	"fmt"
	"sort"

	"github.com/envoyproxy/envoy/contrib/golang/common/go/api"

	"github.com/istio-llm-filter/pkg/types"
)

// Config 定义 LLM Proxy Filter 的配置
type Config struct {
	// Protocol 输入协议类型 (openai)
	Protocol string `json:"protocol"`
	// Algorithm 负载均衡算法 (inference_lb, random, round_robin)
	Algorithm string `json:"algorithm"`
	// ModelMappingRule 模型路由规则映射
	ModelMappingRule map[string]*Rules `json:"model_mapping_rule"`
	// LbMappingRule 负载均衡配置映射
	LbMappingRule map[string]*LBConfig `json:"lb_mapping_rule"`
	// Log 日志配置
	Log *LogConfig `json:"log,omitempty"`
}

// GetProtocol 获取协议
func (c *Config) GetProtocol() string {
	return c.Protocol
}

// GetAlgorithm 获取负载均衡算法
func (c *Config) GetAlgorithm() string {
	if c.Algorithm == "" {
		return string(types.InferenceLB)
	}
	return c.Algorithm
}

// GetModelMappingRule 获取模型映射规则
func (c *Config) GetModelMappingRule() map[string]*Rules {
	return c.ModelMappingRule
}

// GetLbMappingRule 获取负载均衡配置映射
func (c *Config) GetLbMappingRule() map[string]*LBConfig {
	return c.LbMappingRule
}

// GetLog 获取日志配置
func (c *Config) GetLog() *LogConfig {
	return c.Log
}

// Rules 规则列表（用于支持 map 中的 repeated 值）
type Rules struct {
	Rules []*Rule `json:"rules"`
}

// GetRules 获取规则列表
func (r *Rules) GetRules() []*Rule {
	if r == nil {
		return nil
	}
	return r.Rules
}

// Rule 定义单个路由规则
type Rule struct {
	// SceneName 场景名称
	SceneName string `json:"scene_name"`
	// ChainName 链名称
	ChainName string `json:"chain_name"`
	// Backend 后端协议类型 (vllm, sglang, triton)
	Backend string `json:"backend"`
	// RouteName 路由名称
	RouteName string `json:"route_name"`
	// Headers 请求头匹配条件
	Headers []*HeaderValue `json:"headers,omitempty"`
	// Subset 后端子集过滤
	Subset []*Subset `json:"subset,omitempty"`
	// Cluster 集群名称
	Cluster string `json:"cluster"`
}

// HeaderValue 请求头键值对
type HeaderValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Subset 后端子集定义
type Subset struct {
	// Name 子集名称
	Name string `json:"name"`
	// Labels 标签选择器
	Labels map[string]string `json:"labels,omitempty"`
	// Lora LoRA 适配器名称
	Lora string `json:"lora,omitempty"`
	// Weight 权重
	Weight int32 `json:"weight,omitempty"`
}

// LBConfig 负载均衡配置
type LBConfig struct {
	// LoadAwareEnable 是否启用负载感知
	LoadAwareEnable bool `json:"load_aware_enable"`
	// CacheAwareEnable 是否启用缓存感知
	CacheAwareEnable bool `json:"cache_aware_enable"`
	// CandidatePercent 候选集百分比 (0-100)
	CandidatePercent int32 `json:"candidate_percent"`
	// RequestLoadWeight 请求负载权重
	RequestLoadWeight int32 `json:"request_load_weight"`
	// PrefillLoadWeight Prefill 负载权重
	PrefillLoadWeight int32 `json:"prefill_load_weight"`
	// CacheRadioWeight 缓存命中率权重
	CacheRadioWeight int32 `json:"cache_radio_weight"`
}

// LogConfig 日志配置
type LogConfig struct {
	// Enabled 是否启用日志
	Enabled bool `json:"enabled"`
	// Path 日志路径
	Path string `json:"path"`
}

// GetEnabled 获取是否启用日志
func (l *LogConfig) GetEnabled() bool {
	if l == nil {
		return false
	}
	return l.Enabled
}

// GetPath 获取日志路径
func (l *LogConfig) GetPath() string {
	if l == nil {
		return ""
	}
	return l.Path
}

// Tuple 包装规则和相关信息
type Tuple struct {
	TargetModel *Rule
}

// SortTuples 按 Headers 长度降序排序
// 确保更具体的匹配规则优先
func SortTuples(tuples []Tuple) {
	sort.Slice(tuples, func(i, j int) bool {
		return len(tuples[i].TargetModel.Headers) > len(tuples[j].TargetModel.Headers)
	})
}

// Mapping 模型到规则的映射
type Mapping struct {
	Tuples []Tuple
}

// LLMProxyConfig 完整的 LLM Proxy 配置
type LLMProxyConfig struct {
	Config
	// ModelMappings 解析后的模型映射
	ModelMappings map[string]*Mapping
	// LbMappingConfigs 解析后的负载均衡配置
	LbMappingConfigs map[string]*LBConfig
	// MC Metadata-Center 客户端
	MC types.MetadataCenter
}

// buildModelMappings 构建模型映射
func buildModelMappings(mappingRules map[string]*Rules) map[string]*Mapping {
	mappings := map[string]*Mapping{}
	for model, r := range mappingRules {
		rules := r.GetRules()
		if len(rules) == 0 {
			continue
		}
		tuples := make([]Tuple, 0, len(rules))
		for _, rule := range rules {
			tuples = append(tuples, Tuple{
				TargetModel: rule,
			})
		}
		SortTuples(tuples)
		mappings[model] = &Mapping{
			Tuples: tuples,
		}
	}
	return mappings
}

// Init 初始化配置
func (c *LLMProxyConfig) Init() error {
	mappingRules := c.GetModelMappingRule()
	if len(mappingRules) > 0 {
		c.ModelMappings = buildModelMappings(mappingRules)
	}
	lbMappingConfigs := c.GetLbMappingRule()
	if len(lbMappingConfigs) > 0 {
		c.LbMappingConfigs = lbMappingConfigs
	}
	return nil
}

// FindLbMappingRule 查找模型对应的负载均衡配置
func (c *LLMProxyConfig) FindLbMappingRule(modelName string) *LBConfig {
	if c.LbMappingConfigs == nil || modelName == "" {
		return nil
	}
	return c.LbMappingConfigs[modelName]
}

// validateRules 验证规则数组
// 同一模型下的所有规则必须满足：
// 1. cluster 必须一致
// 2. backend 可以为空，默认为 triton
// 3. backend 必须一致
func validateRules(rules []*Rule) (string, string, error) {
	if len(rules) == 0 {
		return "", "", errors.New("rules is empty")
	}
	expectedCluster := rules[0].Cluster
	expectedBackend := rules[0].Backend
	if expectedBackend == "" {
		expectedBackend = "triton"
	}

	for i := range rules {
		if rules[i].Cluster != expectedCluster {
			return "", "", fmt.Errorf("mismatched cluster, current=%s, expected=%s", rules[i].Cluster, expectedCluster)
		}
		if rules[i].Backend == "" {
			rules[i].Backend = "triton"
		}
		if rules[i].Backend != expectedBackend {
			return "", "", fmt.Errorf("mismatched backend, current=%s, expected=%s", rules[i].Backend, expectedBackend)
		}
	}

	return expectedCluster, expectedBackend, nil
}

// Parse 解析并验证配置
func (c *LLMProxyConfig) Parse() error {
	// 验证配置
	if c.Protocol == "" {
		return errors.New("protocol is required")
	}

	mappingRules := c.GetModelMappingRule()
	if len(mappingRules) > 0 {
		for key, rule := range mappingRules {
			_, _, err := validateRules(rule.Rules)
			if err != nil {
				return fmt.Errorf("rules validation error, model=%s, err=%v", key, err)
			}
		}
	}
	return nil
}

// GetModelMappings 获取模型映射
func GetModelMappings(modelMappings map[string]*Mapping, modelName string) []Tuple {
	if len(modelMappings) == 0 {
		return nil
	}
	mapping, ok := modelMappings[modelName]
	if !ok {
		return nil
	}
	return mapping.Tuples
}

// GetCandidateRule 根据请求头从候选规则中选择规则
// 如果只有一个候选规则且没有 headers，直接返回
// 否则通过匹配每个规则的 headers 来选择
func GetCandidateRule(targetModelTuple []Tuple, headers api.RequestHeaderMap) *Rule {
	if len(targetModelTuple) == 1 && len(targetModelTuple[0].TargetModel.Headers) == 0 {
		return targetModelTuple[0].TargetModel
	}

	for _, tuple := range targetModelTuple {
		model := tuple.TargetModel
		if len(model.Headers) == 0 {
			// headers 为空，是默认路由，直接返回
			return model
		}

		allMatched := true
		for _, v := range model.Headers {
			if reqHeaderValue, ok := headers.Get(v.Key); !ok || reqHeaderValue != v.Value {
				allMatched = false
				break
			}
		}

		if allMatched {
			return model
		}
	}

	return nil
}
