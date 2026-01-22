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

package loadbalancer

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"math/rand"
	"slices"

	"github.com/envoyproxy/envoy/contrib/golang/common/go/api"

	"github.com/istio-llm-filter/pkg/metadata"
	"github.com/istio-llm-filter/pkg/types"
)

func init() {
	// 注册推理负载均衡器
	RegisterLbType(types.InferenceLB, InferenceLoadBalancerFactory)
}

// InferenceLoadBalancer 推理负载均衡器
// 基于负载感知和缓存感知的多维度评分算法
type InferenceLoadBalancer struct {
	hosts []types.Host
}

// InferenceLoadBalancerFactory 创建推理负载均衡器
func InferenceLoadBalancerFactory(ctx context.Context, hosts []types.Host) types.LoadBalancer {
	return &InferenceLoadBalancer{
		hosts: hosts,
	}
}

// ChooseHost 选择最优主机
// 算法流程：
// 1. 根据 Label Selector 过滤主机
// 2. 如果启用负载感知，查询 Metadata-Center 获取负载统计
// 3. 如果启用缓存感知，查询 KV-Cache 获取缓存命中信息
// 4. 计算每个主机的综合评分
// 5. 选择 Top N% 候选集
// 6. 从候选集中随机选择一个主机
func (lb *InferenceLoadBalancer) ChooseHost(ctx context.Context) types.Host {
	candidateHosts := lb.hosts

	// 1. 根据 Label Selector 过滤主机
	selector := types.GetValueFromCtx(ctx, types.KeyLbSelector, map[string]string{})
	if len(selector) > 0 {
		candidateHosts = filterHostsBySelector(lb.hosts, selector)
		api.LogDebugf("filtered hosts by selector: %v, count: %d", selector, len(candidateHosts))
	}

	if len(candidateHosts) == 0 {
		api.LogWarnf("no candidate hosts after filtering")
		return nil
	}

	clusterName := types.MustGetValueFromCtx[string](ctx, types.KeyClusterName)
	traceId := types.GetValueFromCtx(ctx, types.KeyTraceId, "")

	// 2. 如果启用负载感知，使用多维度评分选择
	if isLoadAwareEnabled(ctx) {
		candNum := candidateNumFromContext(ctx, candidateHosts)
		hosts := lb.GetCandidateByStats(ctx, clusterName, candidateHosts, candNum)
		return chooseFromCandidates(hosts, clusterName, traceId)
	}

	// 否则随机选择
	return chooseFromCandidates(candidateHosts, clusterName, traceId)
}

// GetCandidateByStats 根据负载统计获取候选主机列表
func (lb *InferenceLoadBalancer) GetCandidateByStats(ctx context.Context, clusterName string, hosts []types.Host, candNum int) []types.Host {
	// 获取负载统计
	stats, err := getEndpointStats(ctx, clusterName, hosts)
	if err != nil {
		api.LogErrorf("failed to get endpoint stats for cluster %s: %v", clusterName, err)
		return hosts
	}

	// 获取缓存统计
	var cacheStats map[string]*EndpointCacheStats
	if isCacheAwareEnabled(ctx) {
		cacheStats, err = getCacheStats(ctx)
		if err != nil {
			api.LogInfof("failed to get cache stats for cluster %s: %v", clusterName, err)
		}
	}

	// 合并统计并计算评分
	stats = mergeStatsAndScore(ctx, stats, cacheStats)

	// 按评分降序排序
	slices.SortFunc(stats, compareByScore)

	// 记录候选信息
	traceId := types.GetValueFromCtx(ctx, types.KeyTraceId, "")
	for i, stat := range stats {
		if i < candNum+5 {
			api.LogInfof("[TraceID: %s] candidate %d for cluster %s: %s", traceId, i, clusterName, stat)
		}
	}

	// 返回 Top N 候选
	result := make([]types.Host, 0, candNum)
	for i := 0; i < candNum && i < len(stats); i++ {
		result = append(result, stats[i].Host)
	}

	return result
}

// EndpointStatsWrapper 端点统计包装器
type EndpointStatsWrapper struct {
	Host          types.Host
	EndpointStats *types.EndpointStats
	CacheStats    *EndpointCacheStats

	// 归一化后的负载值
	RequestLoad  float64
	PrefillLoad  float64
	CacheHitRate float64

	// 综合评分
	Score float64
}

// String 返回统计信息的字符串表示
func (s *EndpointStatsWrapper) String() string {
	return fmt.Sprintf("host=%s, score=%.3f, reqLoad=%.3f, prefillLoad=%.3f, cacheHit=%.3f, totalReqs=%d, promptLen=%d",
		s.Host.Ip(), s.Score, s.RequestLoad, s.PrefillLoad, s.CacheHitRate,
		s.EndpointStats.TotalReqs, s.EndpointStats.PromptLength)
}

// EndpointCacheStats 端点缓存统计
type EndpointCacheStats struct {
	NodeIP         string
	CacheHitScore  float64
	CacheHitLength int
}

// getEndpointStats 获取端点负载统计
func getEndpointStats(ctx context.Context, clusterName string, hosts []types.Host) ([]*EndpointStatsWrapper, error) {
	client := metadata.GetClientOrNoop()
	epStats, err := client.QueryLoad(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	result := make([]*EndpointStatsWrapper, len(hosts))
	for i, host := range hosts {
		stat, ok := epStats[host.Ip()]
		if !ok {
			// 没有统计数据的主机，使用默认值
			result[i] = &EndpointStatsWrapper{
				Host: host,
				EndpointStats: &types.EndpointStats{
					TotalReqs:    0,
					PromptLength: 0,
					PrefillReqs:  0,
				},
			}
		} else {
			result[i] = &EndpointStatsWrapper{
				Host:          host,
				EndpointStats: stat,
			}
		}
	}

	return result, nil
}

// getCacheStats 获取缓存统计
func getCacheStats(ctx context.Context) (map[string]*EndpointCacheStats, error) {
	client := metadata.GetClientOrNoop()
	promptHash := types.GetValueFromCtx(ctx, types.KeyPromptHash, []uint64{})
	clusterName := types.GetValueFromCtx(ctx, types.KeyClusterName, "")

	if len(promptHash) == 0 || clusterName == "" {
		return nil, nil
	}

	result, err := client.QueryKVCache(ctx, clusterName, promptHash, metadata.DefaultTopK)
	if err != nil {
		return nil, err
	}

	stats := make(map[string]*EndpointCacheStats, len(result))
	for _, r := range result {
		stats[r.Ip] = &EndpointCacheStats{
			NodeIP:         r.Ip,
			CacheHitScore:  float64(r.Length) / float64(len(promptHash)),
			CacheHitLength: r.Length,
		}
	}

	return stats, nil
}

// mergeStatsAndScore 合并统计数据并计算评分
// 评分公式: Score = W1 * CacheRatio - W2 * RequestLoad - W3 * PrefillLoad
func mergeStatsAndScore(ctx context.Context, loadStats []*EndpointStatsWrapper, cacheStats map[string]*EndpointCacheStats) []*EndpointStatsWrapper {
	// 计算负载范围
	var maxQueueSize float64 = 0
	var minQueueSize float64 = math.MaxFloat64
	maxPromptLength := 1024 // 最小为 1024，prefill 时间在小于 1024 时很小

	for _, stat := range loadStats {
		if stat.EndpointStats != nil {
			size := float64(stat.EndpointStats.TotalReqs)
			maxQueueSize = math.Max(maxQueueSize, size)
			minQueueSize = math.Min(minQueueSize, size)

			if stat.EndpointStats.PromptLength > maxPromptLength {
				maxPromptLength = stat.EndpointStats.PromptLength
			}
		}
	}
	if minQueueSize == math.MaxFloat64 {
		minQueueSize = 0
	}

	// 获取权重配置
	cacheHitWeight := float64(types.GetValueFromCtx(ctx, types.KeyCacheRatioWeight, types.DefaultCacheRatioWeight))
	prefillWeight := float64(types.GetValueFromCtx(ctx, types.KeyLoadPrefillWeight, types.DefaultPrefillLoadWeight))
	configRequestWeight := float64(types.GetValueFromCtx(ctx, types.KeyLoadRequestWeight, types.DefaultRequestLoadWeight))

	// 动态调整请求负载权重：当并发差异大于 5 时，增加权重
	delta := math.Max(2, maxQueueSize-minQueueSize)
	requestLoadWeight := configRequestWeight * math.Ceil(delta/5)

	api.LogDebugf("scoring weights: cache=%.1f, request=%.1f, prefill=%.1f, delta=%.1f",
		cacheHitWeight, requestLoadWeight, prefillWeight, delta)

	// 计算每个端点的评分
	for _, stat := range loadStats {
		// 设置缓存命中率
		stat.CacheHitRate = 0
		if cacheStats != nil {
			if cacheStat, ok := cacheStats[stat.Host.Ip()]; ok {
				stat.CacheStats = cacheStat
				stat.CacheHitRate = cacheStat.CacheHitScore
			}
		}

		// 计算归一化负载
		stat.RequestLoad = 1.0
		stat.PrefillLoad = 0.0

		if stat.EndpointStats != nil {
			// 请求负载归一化: (当前请求数 - 最小请求数) / delta
			size := float64(stat.EndpointStats.TotalReqs) - minQueueSize
			stat.RequestLoad = size / delta

			// Prefill 负载归一化: 当前 Prompt 长度 / 最大 Prompt 长度
			stat.PrefillLoad = float64(stat.EndpointStats.PromptLength) / float64(maxPromptLength)
		}

		// 计算综合评分
		// Score = W1 * cache_ratio - W2 * request_load - W3 * prefill_load
		// 缓存命中率越高越好（正向），请求负载和 Prefill 负载越低越好（负向）
		stat.Score = cacheHitWeight*stat.CacheHitRate - requestLoadWeight*stat.RequestLoad - prefillWeight*stat.PrefillLoad
	}

	return loadStats
}

// compareByScore 按评分降序比较
func compareByScore(a, b *EndpointStatsWrapper) int {
	return cmp.Compare(b.Score, a.Score)
}

// filterHostsBySelector 根据标签选择器过滤主机
func filterHostsBySelector(hosts []types.Host, selector map[string]string) []types.Host {
	var matched []types.Host
	for _, host := range hosts {
		labels := host.Labels()
		allMatch := true
		for k, v := range selector {
			if labels[k] != v {
				allMatch = false
				break
			}
		}
		if allMatch {
			matched = append(matched, host)
		}
	}
	return matched
}

// chooseFromCandidates 从候选主机中随机选择一个
func chooseFromCandidates(hosts []types.Host, clusterName, traceId string) types.Host {
	if len(hosts) == 0 {
		return nil
	}

	i := rand.Intn(len(hosts))
	host := hosts[i]

	api.LogInfof("[TraceID: %s] chose host %d/%d: %s for cluster %s",
		traceId, i+1, len(hosts), host.Address(), clusterName)

	return host
}

// candidateNumFromContext 从 context 获取候选数量
func candidateNumFromContext(ctx context.Context, hosts []types.Host) int {
	percent := types.GetValueFromCtx(ctx, types.KeyCandidatePercent, types.DefaultCandidatePercent)
	// 至少 1 个，最多全部
	candNum := len(hosts) * percent / 100
	if candNum < 1 {
		candNum = 1
	}
	if candNum > len(hosts) {
		candNum = len(hosts)
	}
	return candNum
}

// isLoadAwareEnabled 检查是否启用负载感知
func isLoadAwareEnabled(ctx context.Context) bool {
	if v := ctx.Value(types.KeyLoadAwareEnable); v != nil {
		if enable, ok := v.(bool); ok {
			return enable
		}
	}
	return metadata.IsEnabled()
}

// isCacheAwareEnabled 检查是否启用缓存感知
func isCacheAwareEnabled(ctx context.Context) bool {
	if v := ctx.Value(types.KeyCacheAwareEnable); v != nil {
		if enable, ok := v.(bool); ok {
			return enable
		}
	}
	return metadata.IsCacheEnabled()
}
