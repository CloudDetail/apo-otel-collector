# apo-otel-collector

使用 OpenTelemetry Collector Builder 创建自定义 Collector 实例，用于采集和接收 APO 的数据。

## Components
### Receivers

## Metadataprocessor

自制组件,用于在内存中维护一个K8s信息缓存 (Metasource)
接收输入的Metric,并将所有前缀为`kindling_`的指标,按其中包含的containerId标签填充k8s数据

K8s信息有三种数据来源:

1. 从K8sAPI中直接获取K8s信息
2. 从另一个可达的Metasource中拉取最新的K8s信息
3. 启动一个网络服务,监听从另一个Metasource推送过来的数据

同时提供两种输出K8s信息的方式:

1. 启动一个网络服务,允许其他Metasource从本实例获取K8s信息
2. 将本实例获取的所有K8s信息向指定的另一个Metasource推送

另外可以提供一个查询端口,其他服务可以从指定端口查询存储的K8s元数据信息
程序内部可以使用cache.Querier接口查询存储的K8s元数据

配置文件示例:

```yaml
processors:
  metadata:
    # 需要填充K8s信息的指标前缀
    metric_prefix: "kindling_"
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
