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

package types

// 负载均衡 Context 键定义
const (
	// KeyFilterCallback Filter 回调处理器
	KeyFilterCallback LBCtxKey = "lb.filterCallback"
	// KeyClusterName 集群名称
	KeyClusterName LBCtxKey = "lb.clusterName"
	// KeyBackendName 后端名称
	KeyBackendName LBCtxKey = "lb.backendName"
	// KeyModelName 模型名称
	KeyModelName LBCtxKey = "lb.modelName"
	// KeyTraceId 链路追踪 ID
	KeyTraceId LBCtxKey = "lb.traceId"
	// KeyPromptHash Prompt 哈希值数组
	KeyPromptHash LBCtxKey = "lb.promptHash"
	// KeyHostMatchInfo 主机匹配信息
	KeyHostMatchInfo LBCtxKey = "lb.hostMatchInfo"
	// KeyLbSelector 负载均衡选择器标签
	KeyLbSelector LBCtxKey = "lb.selector"

	// KeyLoadAwareEnable 是否启用负载感知
	KeyLoadAwareEnable LBCtxKey = "lb.load_aware_enable"
	// KeyCacheAwareEnable 是否启用缓存感知
	KeyCacheAwareEnable LBCtxKey = "lb.cache_aware_enable"
	// KeyCandidatePercent 候选集百分比
	KeyCandidatePercent LBCtxKey = "lb.candidate_percent"
	// KeyCacheRatioWeight 缓存命中率权重
	KeyCacheRatioWeight LBCtxKey = "lb.cache_ratio_weight"
	// KeyLoadRequestWeight 请求负载权重
	KeyLoadRequestWeight LBCtxKey = "lb.request_load_weight"
	// KeyLoadPrefillWeight Prefill 负载权重
	KeyLoadPrefillWeight LBCtxKey = "lb.prefill_load_weight"
)

// 日志字段键定义
const (
	// KeyCacheDuration 缓存查询耗时
	KeyCacheDuration = "cache_duration"
	// KeyUseMetaCache 是否使用 metadata 缓存
	KeyUseMetaCache = "use_cache"
	// KeyLoadDuration 负载查询耗时
	KeyLoadDuration = "load_duration"
	// KeyUseMetaLoad 是否使用 metadata 负载
	KeyUseMetaLoad = "use_load"
)

// 默认权重配置
const (
	// DefaultCacheRatioWeight 默认缓存命中率权重
	DefaultCacheRatioWeight = 2
	// DefaultRequestLoadWeight 默认请求负载权重
	DefaultRequestLoadWeight = 1
	// DefaultPrefillLoadWeight 默认 Prefill 负载权重
	DefaultPrefillLoadWeight = 3
	// DefaultCandidatePercent 默认候选集百分比
	DefaultCandidatePercent = 5
)

// HostMatchInfo 主机匹配信息
type HostMatchInfo struct {
	// CacheRatio 缓存命中率
	CacheRatio float64 `json:"cache_ratio"`
}

// GetValueFromCtx 从 Context 中获取值，如果不存在则返回默认值
func GetValueFromCtx[T any](ctx interface{ Value(any) any }, key LBCtxKey, defaultValue T) T {
	if v := ctx.Value(key); v != nil {
		if val, ok := v.(T); ok {
			return val
		}
	}
	return defaultValue
}

// MustGetValueFromCtx 从 Context 中获取值，如果不存在则 panic
func MustGetValueFromCtx[T any](ctx interface{ Value(any) any }, key LBCtxKey) T {
	if v := ctx.Value(key); v != nil {
		if val, ok := v.(T); ok {
			return val
		}
	}
	var zero T
	return zero
}
