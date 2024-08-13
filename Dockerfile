FROM golang:1.21.5-bullseye AS builder

WORKDIR /build
ENV GOPROXY https://goproxy.cn
COPY go.mod go.sum ./
COPY pkg ./pkg
RUN go mod download && go mod verify

COPY . .
RUN go build -v -o otelcol ./cmd

FROM debian:bullseye-slim AS runner
WORKDIR /app
ENV TZ=Asia/Shanghai
COPY --from=builder /build/otelcol /app/
COPY otelcol-config.yaml /app/conf/otelcol-config.yaml
CMD ["./otelcol", "--config", "/app/conf/otelcol-config.yaml"]