# apo-otel-collector

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
- [backsamplingprocessor](./pkg/processor/backsamplingprocessor)

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
Cache K8s information (Metasource) in memory. It receives input Metrics and fills in the K8s information for all metrics which has matches `container_id` label. 

There are three sources of K8s information: 
1. Directly obtain K8s information from the K8s API.
2. Pull the latest K8s information from another accessible Metasource.
3. Start a network service to listen for data pushed from another Metasource. 

There are also have two ways of output K8s information:
1. Start a network service to allow other Metasources to obtain K8s information from this instance.
2. Push all the K8s information obtained by this instance to the specified other Metasource. 

A query port is provided for other services to query the stored K8s metadata information from the specified port. Use `cache.Querier` API to query the stored K8s metadata.
```yaml
processors:
  metadata:
    metric_prefix: "apo_"
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
Generate Red Metrics of Server / DB / External / Mq.

```yaml
connectors:
  redmetrics:
    server_enabled: true
    db_enabled: true
    external_enabled: true
    mq_enabled: true
    client_entry_url_enabled: false
    cache_entry_url_time: 30s
    unmatch_url_expire_time: 60s
    dimensions_cache_size: 1000
    metrics_flush_interval: 60s
    max_services_to_track: 256
    max_operations_to_track_per_service: 2048
    # vm(VictoriaMetrics) or prom(Promethues)
    metrics_type: "vm"
    latency_histogram_buckets: [5ms, 10ms, 20ms, 30ms, 50ms, 80ms, 100ms, 150ms, 200ms, 300ms, 400ms, 500ms, 800ms, 1200ms, 3s, 5s, 10s, 15s, 20s, 30s, 40s, 50s, 60s]
    # httpMethod / topUrl
    http_parser: topUrl
```

## FillProcExtension
Add Pid and containerId for SkywalkingReceiver and OtelRecevier.

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

## BackSamplingProcessor
* Notify and subscribe sampled traceIds from Receiver.
* Notify Ebpf Agent to collect profiles.
* Notify ilogtail to collect logs.
* Adaptive Sampling, sache and store sampled traces.
* Generate SampledCount Metrics.

```yaml
receivers:
  prometheus/own_metrics:
    config:
      scrape_configs:
        - job_name: 'otel-collector'
          scrape_interval: 10s
          static_configs:
            - targets: ['0.0.0.0:1778']
processors:
  batch:
    send_batch_size: 10000
    timeout: 2s
  backsampling:
    # Notify EbpfAgent Collect OnOffMetric and Profile. It will be disabled when set to zero.
    ebpf_port: 0
    adaptive:
      enable: false
      span_slow_threshold: 10s
      service_sample_window: 1s
      service_sample_count: 1
      memory_check_interval: 2s
      memory_limit_mib_threshold: 200
      traceid_holdtime: 60s
    sampler:
      log_enable: true
      normal_top_sample: false
      normal_sample_wait_time: 5
      open_slow_sampling: true
      open_error_sampling: true
      enable_tail_base_profiling: true
      sample_trace_repeat_num: 3
      sample_trace_wait_time: 30
      sample_trace_ignore_threshold: 0
      silent_period: 5
      silent_count: 1
      silent_mode: window
    controller:
      host: 10.0.2.4
      port: 19090
      interval_query_slow_threshold: 30
      interval_query_sample: 2
      interval_send_trace: 1
    notify:
      enabled: false
service:
  telemetry:
    metrics:
      level: basic
      address: ":1778"
  pipelines:
    traces:
      receivers: [otlp, skywalking]
      processors: [backsampling, batch]
      exporters: [otlp]
    metrics/own_metrics:
      receivers: [prometheus/own_metrics]
      exporters: [otlphttp/victoriametrics]
```