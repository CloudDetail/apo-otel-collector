# apo-otel-collector

使用 OpenTelemetry Collector Builder 创建自定义 Collector 实例，用于采集和接收 APO 的数据。

## Components
### Receivers
- [otlpreceiver](https://github.com/open-telemetry/opentelemetry-collector/tree/main/receiver/otlpreceiver)
- [prometheusreceiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/prometheusreceiver)
- [k8seventsreceiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/k8seventsreceiver)

### Processors
- [batchprocessor](https://github.com/open-telemetry/opentelemetry-collector/tree/main/processor/batchprocessor)
- [memorylimiterprocessor](https://github.com/open-telemetry/opentelemetry-collector/tree/main/processor/memorylimiterprocessor)
- [metadataprocessor](./pkg/processor/metadataprocessor)

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
