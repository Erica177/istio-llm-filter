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

import (
	"context"
	"encoding/json"
)

// EndpointStats 表示后端端点的负载统计信息
type EndpointStats struct {
	// PrefillReqs 正在进行 prefill 阶段或等待队列中的请求数
	PrefillReqs int `json:"prefill_reqs"`
	// TotalReqs 未完成的总请求数
	TotalReqs int `json:"total_reqs"`
	// PromptLength 当前正在处理的 prompt 总长度
	PromptLength int `json:"prompt_length"`
}

// String 返回 EndpointStats 的 JSON 字符串表示
func (e *EndpointStats) String() string {
	b, _ := json.Marshal(e)
	return string(b)
}

// KVCacheLocation 表示 prompt hash 的缓存位置信息
type KVCacheLocation struct {
	// Ip 缓存所在的节点 IP
	Ip string `json:"ip"`
	// Length 缓存命中的长度（hash 块数）
	Length int `json:"length"`
}

// InferenceLoadStats 定义推理负载统计接口
type InferenceLoadStats interface {
	// AddRequest 向 metadata center 添加请求统计
	// requestId: 请求唯一标识
	// cluster: 集群名称
	// ip: 处理请求的后端 IP
	// promptLength: prompt 长度
	AddRequest(ctx context.Context, requestId, cluster, ip string, promptLength int) error

	// DeleteRequest 从 metadata center 删除请求统计
	DeleteRequest(ctx context.Context, requestId string) error

	// DeleteRequestPrompt 将请求的 prompt 长度减少（首 token 到达时调用）
	DeleteRequestPrompt(ctx context.Context, requestId string) error

	// QueryLoad 查询集群的负载统计
	// 返回集群中所有存活 IP 的负载统计
	QueryLoad(ctx context.Context, cluster string) (map[string]*EndpointStats, error)
}

// KVCacheIndexer 定义 KV-Cache 索引接口
type KVCacheIndexer interface {
	// SaveKVCache 保存 prompt hash 的缓存位置到 metadata center
	SaveKVCache(ctx context.Context, cluster, ip string, promptHash []uint64) error

	// QueryKVCache 从 metadata center 查询 prompt hash 的缓存位置
	// topK: 返回前 K 个最匹配的结果
	QueryKVCache(ctx context.Context, cluster string, promptHash []uint64, topK int) ([]*KVCacheLocation, error)
}

// MetadataCenter 定义 Metadata-Center 完整接口
type MetadataCenter interface {
	InferenceLoadStats
	KVCacheIndexer
}
