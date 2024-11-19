# apo-otel-collector

使用 OpenTelemetry Collector Builder 创建自定义 Collector 实例，用于采集和接收 APO 的数据。

## Components
### Receivers
- [otlpreceiver](./pkg/receiver/otlpreceiver)
- [skywalkingreceiver](./pkg/receiver/skywalkingreceiver)
- [prometheusreceiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/prometheusreceiver)
- [k8seventsreceiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/k8seventsreceiver)

### Processors
- [batchprocessor](https://github.com/open-telemetry/opentelemetry-collector/tree/main/processor/batchprocessor)
- [memorylimiterprocessor](https://github.com/open-telemetry/opentelemetry-collector/tree/main/processor/memorylimiterprocessor)
- [metadataprocessor](./pkg/processor/metadataprocessor)

### Connectors
- [redmetricsconnector](./pkg/connector/redmetricsconnector)

### Extensions
- [fillprocextension](./pkg/extension/fillprocextension)

### Exporters
- [debugexporter](https://github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/debugexporter)
- [otlpexporter](https://github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/otlpexporter)
- [otlphttpexporter](https://github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/otlphttpexporter)
- [loggingexporter](https://github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/loggingexporter)
- [clickhouseexporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/clickhouseexporter)
- [prometheusexporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/prometheusexporter)

## Metadataprocessor

自制组件，用于在内存中维护一个K8s信息缓存 (Metasource)。接收输入的Metric，并将所有前缀匹配的指标，按其中包含的`container_id`标签填充k8s信息。

K8s信息有三种数据来源:

1. 从 K8s API 中直接获取 K8s 信息
2. 从另一个可达的 Metasource 中拉取最新的 K8s 信息
3. 启动一个网络服务，监听从另一个 Metasource 推送过来的数据

同时提供两种输出 K8s 信息的方式:

1. 启动一个网络服务，允许其他 Metasource 从本实例获取 K8s 信息
2. 将本实例获取的所有 K8s 信息向指定的另一个 Metasource 推送

另外可以提供一个查询端口，其他服务可以从指定端口查询存储的 K8s 元数据信息。程序内部可以使用 cache.Querier 接口查询存储的 K8s 元数据。

配置文件示例:

```yaml
processors:
  metadata:
    # 需要填充K8s信息的指标前缀
    metric_prefix: "apo_"
    # k8s数据源
    kube_source:
      # kube_auth_type, support serviceAccount and kubeConfig, default is serviceAccount
      kube_auth_type: serviceAccount
      # kube_auth_config, kubeConfig file path, only used when kube_auth_type is kubeConfig
      kube_auth_config: ~/.kube/config
      # cluster_id, setup cluster id in kube metadata
      cluster_id: ""

    exporter:
      # remote_write_addr, push kube metadata to other server, remove if not need
      remote_write_addr: localhost:8080
      # fetch_server_port, allowed other client fetch from this port, remove if not need
      fetch_server_port: 80
```

## Redmetricsconnector
基于SpanMetrics进行二次开发，分桶数据基于ViectoryMetric的histogram库进行计算。
最终生成 服务端RED指标 和 DB调用的RED指标

配置文件示例:
```yaml
connectors:
  redmetrics:
    # 开启服务端RED指标生成
    server_enabled: true
    # 开启DB RED指标生成
    db_enabled: true
    # 开启HTTP/RPC对外调用 RED指标生成
    external_enabled: true
    # 开启MQ RED指标生成
    mq_enabled: true
    # RED Key缓存的数量，超过则清理；用于清理已失效PID数据.
    dimensions_cache_size: 1000
    # 指标发送间隔
    metrics_flush_interval: 60s
    # 最大监控服务数，超过则将服务名打标为 overflow_service.
    max_services_to_track: 256
    # 每个服务下最大URL数，超过则将URL打标为 overflow_operation.
    max_operations_to_track_per_service: 2048
    # vm(VictoriaMetrics) or prom(Promethues)
    metrics_type: "vm"
    # Promethues场景下需指定分桶.
    latency_histogram_buckets: [5ms, 10ms, 20ms, 30ms, 50ms, 80ms, 100ms, 150ms, 200ms, 300ms, 400ms, 500ms, 800ms, 1200ms, 3s, 5s, 10s, 15s, 20s, 30s, 40s, 50s, 60s]
    # URL收敛算法：httpMethod / topUrl
    http_parser: topUrl
```

## FillProcExtension
FillProcExtension插件 基于Peer信息获取PID 和 ContainerId信息
在原SkywalkingReceiver、OtelRecevier 基础上增加PID、ContainerId信息

配置示例
```yaml
extensions:
  fill_proc:
    enable: true
    interval: 5s
receivers:
  skywalking:
    fillproc_extension: fill_proc
    protocols:
      grpc:
        endpoint: 0.0.0.0:11800
      http: 
        endpoint: 0.0.0.0:12800
  otlp:
    fillproc_extension: fill_proc
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
        max_recv_msg_size_mib: 999999999
      http:
        endpoint: 0.0.0.0:4318
service:
  extensions: [fill_proc]
  pipelines:
    traces:
      receivers: [skywalking, otlp]
```