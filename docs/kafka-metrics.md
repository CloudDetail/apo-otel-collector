# Kafka eBPF 指标接入

本仓库集成官方 `kafkareceiver v0.106.1`，无需另写消费服务。
使用重新构建的 Collector，加载 [独立配置](../deploy/kafka-metrics.yaml)：

```text
电子云 Collector → Kafka → apo-otel-collector → Prometheus Remote Write → VictoriaMetrics
```

## 消息约定

- Kafka message value 必须是标准 OTLP protobuf `ExportMetricsServiceRequest`，配置为 `encoding: otlp_proto`。
- 当前版本的 Kafka metrics receiver **不支持 `otlp_json`**，也不接受自定义 JSON 包装、base64 包装或带 HTTP/gRPC 帧的正文。电子云侧若收到 camera-agent 的 OTLP/HTTP JSON，应通过 Collector Kafka exporter 以 `otlp_proto` 重新编码后发送。
- 一个 Topic 只承载 metrics，不混入 traces/logs。保留指标名、原始采样时间戳、单位、累计值语义及对象标签。
- camera-agent 的 `_bucket` / `_count` / `_sum` 序列按原样传递；`vmrange` 和 `le` 不能互换。Exporter 保留 `add_metric_suffixes: false`，避免名称追加 `_total` 等后缀。
- 资源属性会转成指标标签；`service.name` 等标准属性还可能生成 `job` / `instance`。应核对最终标签。携带 `tenantId` 标签本身不等于实现查询权限隔离。

## 构建与启动

```sh
GOTOOLCHAIN=go1.21.5 go build -o otelcol ./cmd

# 以下为示例值，启动前替换为现场参数。
export KAFKA_BROKER='kafka.example.internal:9092'
export KAFKA_PROTOCOL_VERSION='2.0.0'
export KAFKA_METRICS_TOPIC='ebpf-metrics'
export KAFKA_CONSUMER_GROUP='apo-ebpf-metrics'
export VM_REMOTE_WRITE_URL='http://victoria-metrics:8428/api/v1/write'

./otelcol validate --config deploy/kafka-metrics.yaml
./otelcol --config deploy/kafka-metrics.yaml
```

`KAFKA_PROTOCOL_VERSION` 应按实际 broker 支持的协议设置。多个 broker 可直接将 `brokers` 改成 YAML 数组，不能在单个环境变量中用逗号拼接地址。`VM_REMOTE_WRITE_URL` 必须是完整的 Remote Write 地址；VM 集群通常为 `http://<vminsert>:8480/insert/<accountID>/prometheus/api/v1/write`。

所有副本使用同一消费组以分摊分区；消费组名保持稳定。`initial_offset: earliest` 仅在没有已提交 offset 时生效，会读取仍保留的历史消息；若只需新数据，可改为 `latest`。

构建镜像可继续使用仓库 Dockerfile：

```sh
docker build -t apo-otel-collector:kafka-metrics .
```

镜像默认配置保持原有用途。部署时将本示例挂载为 `/app/conf/otelcol-config.yaml`，传入上述环境变量，并使用新镜像。Helm 场景需要相应更新 Gateway ConfigMap、环境变量和镜像 tag；本次仓库改动不会自动更新已部署实例。

如需合入已有 Gateway 配置，将 `kafka/ebpf`、`prometheusremotewrite/ebpf`、`metrics/kafka` 三段加入对应位置。此 pipeline 不添加本地 `resource/cluster_id` 或 `metadata` processor，避免覆盖远端对象归属。

## Kafka 认证

需要 SASL/TLS 时，在 `receivers.kafka/ebpf` 中增加以下配置，并挂载证书、通过 Secret 注入凭据；机制以现场要求为准：

```yaml
auth:
  sasl:
    mechanism: SCRAM-SHA-512
    username: "${env:KAFKA_USERNAME}"
    password: "${env:KAFKA_PASSWORD}"
  tls:
    ca_file: /etc/kafka/ca.crt
```

## 消费确认与验证边界

示例使用同步 Remote Write，关闭 exporter 内存队列且不接异步 batch processor；`message_marking.after: true`、`on_error: false` 使消息在下游成功返回后才被标记，随后自动提交 offset。吞吐需求更高时可增加分区和副本；若另加异步队列或 batch，需要重新评估故障时的数据丢失窗口。

这不是端到端恰好一次：写入成功而 offset 尚未提交时重启，可能重放消息。无效消息或持续写入失败会阻塞分区，应关注 Collector 错误日志与消费延迟，不应直接跳过未知数据。保留原采样时间，并避免同一批指标同时通过抓取和 Kafka 两条路径入库。

`validate` 只检查配置与组件，不连接 Kafka/VM，也不能证明实际消息可解码。现场验收应使用真实样例确认消费组 offset 前进、VM 可查询到原指标名及标签，并验证累计计数和桶数据；Kafka 认证、断连恢复、现场租户及对象关联仍需联调。

## 本地验证记录（2026-09-28）

- 分支：`feat/kafka-metrics-receiver`。
- Go 1.21.5 / darwin arm64：`go build -o otelcol ./cmd` 通过，与 Dockerfile 的 Go 版本一致。
- 实际二进制 `components` 输出包含 `kafka`，metrics 稳定性标为 Beta。
- 注入本地示例环境变量后，`validate --config deploy/kafka-metrics.yaml` 通过；原有 `otelcol-config.yaml` 校验也通过。
- 构建清单、组件注册与直接依赖的 26 个组件一致；`git diff --check` 通过。
- `go test ./...` 通过，但根模块只有 `cmd` 且无测试文件，不能将其作为 Kafka 消费功能测试。
- macOS 原始 Go 二进制启动停滞后，对本地二进制及临时副本执行 ad-hoc 签名并完成上述运行校验；未改变源码或系统安全设置。
- 本机 Docker daemon 未运行；未构建/发布镜像，未连接实际 Kafka 或 VictoriaMetrics，未进行现场验收。
