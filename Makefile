OS := $(shell uname)

.PHONY: otelcol
otelcol:
	go build -o otelcol ./cmd

check_builder_exist:
	@if command -v builder >/dev/null 2>&1; then \
		echo "using builder to generate otelcol code"; \
	else \
		echo "builder is not installed. Installed it with: go install go.opentelemetry.io/collector/cmd/builder@v0.103.1"; \
		exit 1; \
	fi

.PHONY: otelcol-cmd
otelcol-cmd: check_builder_exist
	builder --config=./.builder/otelcol-builder.yaml --skip-compilation
	@mv ./cmd/go.mod go.mod && mv ./cmd/go.sum go.sum
ifeq ($(OS), Darwin)
	@sed -i '' 's#module go.opentelemetry.io/collector/cmd/builder#module github.com/CloudDetail/apo-otel-collector#g' go.mod
	@sed -i '' 's#=> ../pkg/#=> ./pkg/#g' go.mod
else
	@sed -i 's#module go.opentelemetry.io/collector/cmd/builder#module github.com/CloudDetail/apo-otel-collector#g' go.mod
	@sed -i 's#=> ../pkg/#=> ./pkg/#g' go.mod
endif
