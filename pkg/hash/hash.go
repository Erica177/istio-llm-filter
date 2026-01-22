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

// Package hash 提供 Prompt 哈希计算功能
// 用于 KV-Cache 感知的负载均衡
package hash

import (
	"hash"

	"github.com/twmb/murmur3"
)

const (
	// DefaultTextChunkLen 默认文本分块长度
	// 每 512 字符计算一个哈希值
	DefaultTextChunkLen = 512
)

// Config 哈希配置
type Config struct {
	// ChunkLen 分块长度
	ChunkLen int `json:"chunk_len"`
}

// Hash Prompt 哈希计算器
type Hash struct {
	config  *Config
	newHash func() hash.Hash64
}

// New 创建新的哈希计算器
func New(config *Config) *Hash {
	if config == nil {
		config = &Config{ChunkLen: DefaultTextChunkLen}
	}
	if config.ChunkLen <= 0 {
		config.ChunkLen = DefaultTextChunkLen
	}
	return &Hash{
		config:  config,
		newHash: murmur3.New64,
	}
}

// PromptToHash 将 Prompt 内容转换为哈希值数组
// 每个 ChunkLen 长度的内容生成一个哈希值
// 返回的哈希数组用于 KV-Cache 位置查询
func (h *Hash) PromptToHash(prompt []byte) []uint64 {
	plen := len(prompt)
	if plen == 0 {
		return []uint64{}
	}

	// 计算分块数量
	numChunks := (plen + h.config.ChunkLen - 1) / h.config.ChunkLen

	buf := make([]uint64, 0, numChunks)
	hasher := h.newHash()

	// 如果内容小于等于一个分块长度，直接计算整体哈希
	if plen <= h.config.ChunkLen {
		hasher.Write(prompt)
		buf = append(buf, hasher.Sum64())
		return buf
	}

	// 按分块计算哈希
	// 注意：这里是累积哈希，每个块的哈希包含之前所有块的内容
	// 这样可以支持前缀匹配查询
	for start := 0; start < plen; start += h.config.ChunkLen {
		end := start + h.config.ChunkLen
		if end > plen {
			end = plen
		}
		chunk := prompt[start:end]
		hasher.Write(chunk)
		buf = append(buf, hasher.Sum64())
	}

	return buf
}

// GetChunkLen 获取分块长度
func (h *Hash) GetChunkLen() int {
	return h.config.ChunkLen
}
