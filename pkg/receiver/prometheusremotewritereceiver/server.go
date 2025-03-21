// Copyright 2020, OpenTelemetry Authors
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
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/gorilla/mux"
	"github.com/prometheus/prometheus/prompb"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type prometheusRemoteWriteServer struct {
	*http.Server
	*serverConfig
	closeChannel *sync.Once
	listening    *sync.WaitGroup
}

type serverConfig struct {
	component.TelemetrySettings
	Reporter reporter
	component.Host
	Mc     chan<- pmetric.Metrics
	Parser *prometheusRemoteOtelParser
	Path   string
	confighttp.ServerConfig
}

func newPrometheusRemoteWriteServer(ctx context.Context, config *serverConfig) (*prometheusRemoteWriteServer, error) {
	mx := mux.NewRouter()
	handler := newHandler(config.Parser, config, config.Mc)
	mx.HandleFunc(config.Path, handler)
	mx.Host(config.ServerConfig.Endpoint)
	server, err := config.ServerConfig.ToServer(ctx, config.Host, config.TelemetrySettings, mx,
		// ensure we support the snappy Content-Encoding, but leave it to the prometheus remotewrite lib to decompress.
		confighttp.WithDecoder("snappy", func(body io.ReadCloser) (io.ReadCloser, error) {
			return body, nil
		}))
	server.Addr = config.ServerConfig.Endpoint
	if err != nil {
		return nil, err
	}
	prwServer := &prometheusRemoteWriteServer{
		Server:       server,
		serverConfig: config,
		closeChannel: &sync.Once{},
		listening:    &sync.WaitGroup{},
	}
	prwServer.listening.Add(1)
	return prwServer, nil
}

func (prw *prometheusRemoteWriteServer) close() error {
	defer prw.closeChannel.Do(func() { close(prw.Mc) })
	return prw.Server.Close()
}

func (prw *prometheusRemoteWriteServer) listenAndServe(ctx context.Context) error {
	prw.Reporter.OnDebugf("Starting prometheus simple write server")
	listener, err := prw.serverConfig.ServerConfig.ToListener(ctx)
	if err != nil {
		return err
	}
	defer listener.Close()
	prw.listening.Done()
	err = prw.Server.Serve(listener)
	prw.listening.Add(1)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func newHandler(parser *prometheusRemoteOtelParser, sc *serverConfig, mc chan<- pmetric.Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sc.Reporter.OnDebugf("Processing write request %s", r.RequestURI)
		req, err := DecodeWriteRequest(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Timeseries) == 0 && len(req.Metadata) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		results, err := parser.fromPrometheusWriteRequestMetrics(req)
		if nil != err {
			http.Error(w, err.Error(), http.StatusBadRequest)
			sc.Reporter.OnDebugf("prometheus_translation", err)
			return
		}
		mc <- results
		w.WriteHeader(http.StatusAccepted)
	}
}

// DecodeWriteRequest from an io.Reader into a prompb.WriteRequest, handling
// snappy decompression.
func DecodeWriteRequest(r io.Reader) (*prompb.WriteRequest, error) {
	compressed, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	reqBuf, err := snappy.Decode(nil, compressed)
	if err != nil {
		return nil, err
	}

	var req prompb.WriteRequest
	if err := proto.Unmarshal(reqBuf, &req); err != nil {
		return nil, err
	}

	return &req, nil
}
