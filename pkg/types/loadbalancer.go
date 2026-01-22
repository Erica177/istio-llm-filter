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

// Package types 定义了 Istio LLM Filter 的公共类型
package types

import "context"

// CtxKey 定义 Context 中使用的键类型
type CtxKey string

// LBCtxKey 定义负载均衡 Context 中使用的键类型
type LBCtxKey string

const (
	// CtxKeyTraceID 用于在 Context 中传递 Trace ID
	CtxKeyTraceID CtxKey = "trace_id"
)

// LoadBalancerType 定义负载均衡器类型
type LoadBalancerType string

const (
	// Random 随机负载均衡
	Random LoadBalancerType = "Random"
	// RoundRobin 轮询负载均衡
	RoundRobin LoadBalancerType = "RoundRobin"
	// InferenceLB 推理负载均衡（多维度评分）
	InferenceLB LoadBalancerType = "inference_lb"
)

// Host 表示一个后端服务实例
type Host interface {
	// Ip 返回主机 IP 地址
	Ip() string
	// Port 返回主机端口
	Port() uint32
	// Address 返回 "ip:port" 格式的地址
	Address() string
	// Labels 返回主机标签，用于子集路由
	Labels() map[string]string
}

// GlobalLoadBalancer 全局负载均衡器，从多个集群中选择主机
type GlobalLoadBalancer interface {
	// ChooseHost 从指定集群中选择一个主机
	ChooseHost(ctx context.Context, cluster string, lbType LoadBalancerType) (Host, error)
}

// LoadBalancer 集群内负载均衡器，从单个集群中选择主机
type LoadBalancer interface {
	// ChooseHost 选择一个主机
	ChooseHost(ctx context.Context) Host
}

// LoadBalancerFactory 负载均衡器工厂函数类型
type LoadBalancerFactory func(ctx context.Context, hosts []Host) LoadBalancer
