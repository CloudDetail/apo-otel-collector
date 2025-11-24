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
- [traceblockprocessor](./pkg/processor/traceblockprocessor)

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
      normal_sample_wait_time: 60
      open_slow_sampling: true
      open_error_sampling: true
      enable_tail_base_profiling: true
      sample_trace_repeat_num: 3
      sample_trace_wait_time: 30
      sample_trace_ignore_threshold: 0
      sample_trace_slow_threshold_ms: 100
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

## TraceBlockProcessor
Drops span data from traces that have been continuously generating spans for an extended period. This processor:

1. **Caches the start time and latest update time for each TraceId**: When a TraceId is first encountered, it records the first seen time (firstSeen) and latest update time (lastSeen).
2. **Periodically cleans expired TraceIds**: Executes a cleanup task every minute (default) to remove TraceId records that have not received new spans within the specified time period (default 5 minutes).
3. **Automatically blocks long-running traces**: When a TraceId has been cached for longer than the specified duration (default 1 hour), all subsequent span data for that TraceId will be directly dropped, but the latest time will continue to be updated. The cleanup duration for blocked TraceIds is increased to 2x (default 2 * 5 minutes = 10 minutes).

This processor effectively prevents abnormally long-running traces from consuming excessive resources, and is particularly suitable for handling abnormal traces that continuously generate spans but cannot properly terminate.

```yaml
processors:
  traceblock:
    # Maximum time to retain a Trace when no new spans are received, default 5 minutes
    idle_ttl: 5m
    # Duration after which a trace in cache is considered abnormal and subsequent spans are dropped, default 1 hour
    block_threshold: 30m
    # Maximum time to retain a Trace for blocked traces, must greater than idle_ttl.
    blocked_idle_ttl: 10m
service:
  pipelines:
    traces:
      receivers: [otlp, skywalking]
      processors: [traceblock, backsampling, batch]
      exporters: [otlp]
```

### Parameter Tuning Recommendations

- **clean_interval**: The default 1 minute is reasonable for most scenarios. You can adjust it based on your cleanup frequency requirements. Shorter intervals provide more timely cleanup but consume more CPU.

- **idle_ttl**: The default 5 minutes is suitable for normal traces. If your application has traces that may have longer gaps between spans (but are still normal), you may need to increase this value. However, be aware that longer TTLs will increase memory usage.

- **block_threshold**: The default 1 hour is designed to catch truly abnormal traces. If your environment has many legitimate long-running traces (e.g., batch jobs, data processing pipelines), you may need to increase this value. Conversely, if you want to block abnormal traces more aggressively, you can decrease it (e.g., 30 minutes). Note: `block_threshold` must be greater than `idle_ttl`.

- **blocked_idle_multiplier**: The default value of 2 means blocked traces will be cleaned after 2 * idle_ttl (10 minutes by default). This gives blocked traces a longer cleanup window to ensure they are truly inactive before removal. Generally, there's no need to adjust this unless you have specific requirements.