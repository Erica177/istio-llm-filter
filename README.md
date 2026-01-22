# istio-llm-filter

Envoy Golang Filter 插件，为 Istio 提供 LLM 请求的智能负载均衡能力。

## 功能特性

- **多维度负载均衡**: 基于请求负载、Prefill 队列深度、KV-Cache 命中率的综合评分算法
- **KV-Cache 感知路由**: 通过 Prompt 哈希实现缓存亲和性，提升推理效率
- **协议转码**: 支持 OpenAI 协议，兼容 vLLM、SGLang、TensorRT 等后端
- **异步元数据同步**: 非阻塞的 Metadata-Center 交互，不影响请求延迟
- **灵活配置**: 支持多模型、多环境的路由规则配置

## 快速开始

### 依赖

- Go 1.22+
- Envoy with Golang Filter support
- Metadata-Center 服务（可选）

### 编译

```bash
# 下载依赖
make deps

# 编译 .so 共享库 (Linux AMD64)
make build

# 本地架构编译
make build-local

# 验证编译
make verify
```

编译产物位于 `build/libllmproxy.so`。

### 部署

1. 将 `libllmproxy.so` 复制到 Envoy Sidecar 可访问的路径
2. 配置 EnvoyFilter 加载插件
3. （可选）部署 Metadata-Center 服务

详细配置请参考 [使用指南](docs/usage.md)。

## 项目结构

```
istio-llm-filter/
├── cmd/                      # 插件入口
│   └── main.go
├── pkg/
│   ├── types/                # 公共类型定义
│   ├── config/               # 配置解析
│   ├── hash/                 # Prompt 哈希计算
│   ├── metadata/             # Metadata-Center 客户端
│   ├── loadbalancer/         # 负载均衡算法
│   ├── transcoder/           # 协议转码器
│   │   └── openai/           # OpenAI 协议实现
│   └── filter/               # 核心过滤器
├── docs/                     # 文档
│   ├── design.md             # 设计文档
│   ├── flowchart.md          # 执行流程图
│   └── usage.md              # 使用指南
├── examples/                 # 示例配置
│   └── envoyfilter.yaml
├── Makefile
└── go.mod
```

## 文档

- [设计文档](docs/design.md) - 架构设计和模块说明
- [执行流程图](docs/flowchart.md) - 请求处理和负载均衡流程
- [使用指南](docs/usage.md) - 部署配置和环境变量说明

## 负载均衡算法

插件实现了基于推理负载的多维度评分算法：

```
Score = W1 × CacheRatio - W2 × NormalizedRequestLoad - W3 × NormalizedPrefillLoad
```

其中：
- `CacheRatio`: KV-Cache 命中率 (0-1)
- `NormalizedRequestLoad`: 归一化的并发请求数
- `NormalizedPrefillLoad`: 归一化的 Prefill 队列深度
- `W1, W2, W3`: 可配置的权重参数

默认权重: W1=2, W2=1, W3=3（优先考虑 Prefill 队列）

## 配置示例

```yaml
config:
  model_mapping_rule:
    - model_name: "qwen-plus"
      rules:
        - headers:
            - name: "x-env"
              exact_match: "prod"
          subsets:
            - cluster: "outbound|8080||qwen-prod.default.svc.cluster.local"
  lb_mapping_rule:
    - model_name: "qwen-plus"
      lb_config:
        - lb_type: "inference_lb"
          cache_ratio_weight: 2
          request_load_weight: 1
          prefill_load_weight: 3
```

## 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `METADATA_CENTER_HOST` | Metadata-Center 地址 | localhost |
| `METADATA_CENTER_PORT` | Metadata-Center 端口 | 8080 |
| `METADATA_CENTER_ENABLED` | 是否启用 | false |
| `METADATA_CENTER_CACHE_ENABLED` | 是否启用缓存功能 | false |

## 开发

```bash
# 格式化代码
make fmt

# 代码检查
make lint

# 运行测试
make test

# 测试覆盖率
make test-coverage
```

## License

Apache License 2.0
