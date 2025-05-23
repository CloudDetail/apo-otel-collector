// Copyright Splunk, Inc.
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

package prometheusremotewritereceiver

import (
	"context"
	"fmt"

	"github.com/CloudDetail/apo-otel-collector/pkg/receiver/prometheusremotewritereceiver/internal/metadata"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
)

var _ receiver.Metrics = (*prometheusRemoteWriteReceiver)(nil)

// prometheusRemoteWriteReceiver implements the receiver.Metrics for PrometheusRemoteWrite protocol.
type prometheusRemoteWriteReceiver struct {
	server       *prometheusRemoteWriteServer
	reporter     reporter
	nextConsumer consumer.Metrics
	cancel       context.CancelFunc
	config       *Config
	settings     receiver.Settings
}

func newReceiver(
	settings receiver.Settings,
	config *Config,
	nextConsumer consumer.Metrics,
) (receiver.Metrics, error) {
	rep, err := newOtelReporter(settings)
	if err != nil {
		return nil, err
	}

	r := &prometheusRemoteWriteReceiver{
		settings:     settings,
		config:       config,
		nextConsumer: nextConsumer,
		reporter:     rep,
	}
	return r, nil
}

// Start starts an HTTP server that can process Prometheus Remote Write Requests
func (receiver *prometheusRemoteWriteReceiver) Start(ctx context.Context, host component.Host) error {
	metricsChannel := make(chan pmetric.Metrics, receiver.config.BufferSize)
	cfg := &serverConfig{
		ServerConfig:      receiver.config.ServerConfig,
		Path:              receiver.config.ListenPath,
		Mc:                metricsChannel,
		TelemetrySettings: receiver.settings.TelemetrySettings,
		Reporter:          receiver.reporter,
		Host:              host,
		Parser:            newPrometheusRemoteOtelParser(),
	}
	if receiver.server != nil {
		err := receiver.server.close()
		if err != nil {
			return err
		}
	}
	if receiver.cancel != nil {
		receiver.cancel()
	}
	ctx, receiver.cancel = context.WithCancel(ctx)
	server, err := newPrometheusRemoteWriteServer(ctx, cfg)
	if err != nil {
		return err
	}
	receiver.server = server

	go receiver.startServer(ctx)
	go receiver.manageServerLifecycle(ctx, metricsChannel)

	return nil
}

func (receiver *prometheusRemoteWriteReceiver) startServer(ctx context.Context) {
	prometheusRemoteWriteServer := receiver.server
	if prometheusRemoteWriteServer == nil {
		receiver.settings.TelemetrySettings.ReportStatus(component.NewFatalErrorEvent(fmt.Errorf("start called on null prometheusRemoteWriteServer for receiver %s", metadata.Type)))
	} else if err := prometheusRemoteWriteServer.listenAndServe(ctx); err != nil {
		// our receiver swallows http's ErrServeClosed, and we should only get "concerning" issues at this point in the code.
		receiver.settings.TelemetrySettings.ReportStatus(component.NewFatalErrorEvent(err))
		receiver.reporter.OnDebugf("Error in %s/%s listening on %s/%s: %s", metadata.Type, receiver.settings.ID, prometheusRemoteWriteServer.Addr, prometheusRemoteWriteServer.Path, err)
	}
}

func (receiver *prometheusRemoteWriteReceiver) manageServerLifecycle(ctx context.Context, metricsChannel <-chan pmetric.Metrics) {
	for {
		select {
		case metrics, stillOpen := <-metricsChannel:
			if !stillOpen {
				return
			}
			metricContext := receiver.reporter.StartMetricsOp(ctx)
			err := receiver.flush(metricContext, metrics)
			if err != nil {
				receiver.reporter.OnError(metricContext, "flush_error", err)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// Shutdown stops the PrometheusSimpleRemoteWrite receiver.
func (receiver *prometheusRemoteWriteReceiver) Shutdown(context.Context) error {
	if receiver.cancel == nil {
		return nil
	}
	defer receiver.cancel()
	if receiver.server != nil {
		return receiver.server.close()
	}
	return nil
}

func (receiver *prometheusRemoteWriteReceiver) flush(ctx context.Context, metrics pmetric.Metrics) error {
	err := receiver.nextConsumer.ConsumeMetrics(ctx, metrics)
	receiver.reporter.OnMetricsProcessed(ctx, metrics.DataPointCount(), err)
	return err
}
