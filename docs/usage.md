# Istio LLM Filter 使用说明

## 编译

### 前置要求

- Go 1.22+
- Make

### 编译命令

```bash
cd istio-llm-filter
make build
```

编译完成后生成 `libllmproxy.so` 文件。

## 部署

### 1. 部署 Metadata-Center

确保 Metadata-Center 服务已部署并可访问：

```yaml
apiVersion: v1
kind: Service
metadata:
  name: metadata-center
  namespace: ai-inference
spec:
  ports:
    - port: 80
      targetPort: 8080
  selector:
    app: metadata-center
```

### 2. 配置 Istio Gateway

将编译好的 `.so` 文件挂载到 Istio Ingress Gateway：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-ingressgateway
  namespace: istio-system
spec:
  template:
    spec:
      containers:
        - name: istio-proxy
          volumeMounts:
            - name: llm-filter
              mountPath: /usr/local/envoy/libllmproxy.so
              subPath: libllmproxy.so
      volumes:
        - name: llm-filter
          configMap:
            name: llm-filter-so
```

### 3. 应用 EnvoyFilter

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: llm-proxy-filter
  namespace: istio-system
spec:
  workloadSelector:
    labels:
      istio: ingressgateway
  configPatches:
    - applyTo: HTTP_FILTER
      match:
        context: GATEWAY
        listener:
          filterChain:
            filter:
              name: "envoy.filters.network.http_connection_manager"
              subFilter:
                name: "envoy.filters.http.router"
      patch:
        operation: INSERT_BEFORE
        value:
          name: envoy.filters.http.golang
          typed_config:
            "@type": "type.googleapis.com/envoy.extensions.filters.http.golang.v3alpha.Config"
            library_id: llm-proxy
            library_path: /usr/local/envoy/libllmproxy.so
            plugin_name: llm-proxy
            plugin_config:
              "@type": "type.googleapis.com/xds.type.v3.TypedStruct"
              value:
                protocol: openai
                algorithm: inference_lb
                model_mapping_rule:
                  qwen-2.5-72b:
                    rules:
                      - scene_name: qwen-2.5-72b
                        cluster: "outbound|8000||qwen-service"
                        backend: vllm
                lb_mapping_rule:
                  qwen-2.5-72b:
                    load_aware_enable: true
                    cache_aware_enable: true
                    candidate_percent: 10
                    request_load_weight: 1
                    prefill_load_weight: 3
                    cache_ratio_weight: 2
```

### 4. 配置后端服务

```yaml
apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: qwen-service
  namespace: ai-inference
spec:
  hosts:
    - qwen-service
  ports:
    - name: http
      number: 8000
      protocol: HTTP
  resolution: STATIC
  endpoints:
    - address: 10.0.1.1
    - address: 10.0.1.2
    - address: 10.0.1.3
```

## 环境变量配置

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `METADATA_CENTER_HOST` | localhost | Metadata-Center 地址 |
| `METADATA_CENTER_PORT` | 80 | Metadata-Center 端口 |
| `METADATA_CENTER_FETCH_METRIC_TIMEOUT` | 100 | 负载查询超时 (ms) |
| `METADATA_CENTER_FETCH_CACHE_TIMEOUT` | 100 | 缓存查询超时 (ms) |
| `METADATA_CENTER_UPDATE_STATS_TIMEOUT` | 100 | 统计更新超时 (ms) |
| `METADATA_CENTER_QUEUE_SIZE` | 1000 | 异步队列大小 |
| `METADATA_CENTER_WORKER_COUNT` | 100 | 工作协程数量 |

## 配置参数说明

### model_mapping_rule

模型路由规则，将客户端请求的模型名映射到后端集群：

```yaml
model_mapping_rule:
  <model_name>:          # 客户端请求的模型名
    rules:
      - scene_name: xxx  # 场景名称
        cluster: xxx     # 后端集群名 (Istio 格式)
        backend: vllm    # 后端类型 (vllm/sglang/tensorrt)
        headers:         # 请求头匹配条件 (可选)
          - key: x-env
            value: prod
        subset:          # 后端子集过滤 (可选)
          - name: main
            labels:
              version: v1
            lora: lora-adapter-1  # LoRA 适配器 (可选)
```

### lb_mapping_rule

负载均衡配置：

```yaml
lb_mapping_rule:
  <model_name>:
    load_aware_enable: true    # 启用负载感知
    cache_aware_enable: true   # 启用缓存感知
    candidate_percent: 10      # 候选集百分比 (1-100)
    request_load_weight: 1     # 请求负载权重
    prefill_load_weight: 3     # Prefill 负载权重
    cache_ratio_weight: 2      # 缓存命中权重
```

## 测试

### 发送测试请求

```bash
curl -X POST http://gateway:10000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen-2.5-72b",
    "messages": [
      {"role": "user", "content": "Hello, how are you?"}
    ],
    "stream": true
  }'
```

### 查看日志

```bash
kubectl logs -n istio-system -l istio=ingressgateway -f | grep "llm-proxy"
```

### 验证负载分布

```bash
# 查看选择的后端
kubectl logs -n istio-system -l istio=ingressgateway | grep "selected backend"
```

## 故障排查

### 1. 插件未加载

检查 `.so` 文件是否正确挂载：

```bash
kubectl exec -n istio-system deploy/istio-ingressgateway -- ls -la /usr/local/envoy/
```

### 2. Metadata-Center 连接失败

检查环境变量和网络连通性：

```bash
kubectl exec -n istio-system deploy/istio-ingressgateway -- \
  curl -v http://metadata-center.ai-inference.svc:80/v1/load/stats
```

### 3. 模型未找到

检查 model_mapping_rule 配置是否正确匹配请求中的模型名。

### 4. 负载均衡不生效

- 确认 `load_aware_enable` 设置为 `true`
- 确认 Metadata-Center 返回了有效的负载数据
- 查看日志中的评分计算详情
