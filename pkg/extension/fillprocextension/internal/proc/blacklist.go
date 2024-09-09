package proc

import "strings"

var blackCmdList = []string{
	"/usr/lib/systemd",
	"-bash",
	"/bin/sh",
	"/bin/bash",
}

var blackComms = map[string]bool{
	"sh":              true,
	"bash":            true,
	"sshd":            true,
	"etcd":            true,
	"nginx":           true,
	"nginx-ingress-c": true,
	"sftp-server":     true,
	"NetworkManager":  true,
	"calico-node":     true,
	"pause":           true,
	"sleep":           true,
	"crond":           true,
	"openresty":       true,
	"alertmanager":    true,
	"coredns":         true,
	"kube-apiserver":  true,
	"kube-controller": true,
	"kube-proxy":      true,
	"kube-scheduler":  true,
	"kubelet":         true,
	"kubectl":         true,
	"containerd":      true,
	"containerd-shim": true,
	"docker-proxy":    true,
	"dockerd":         true,
	"odiglet":         true,
	"signoz-collecto": true,
	"runsvdir":        true,
	"runsv":           true,
	"apisix-ingress-": true,
	"clickhouse-oper": true,
	"clickhouse-serv": true,
	"metrics-exporte": true,
	"elastic-operato": true,
	"vmalert-prod":    true,
	"otelcol":         true,
	"otelcol-contrib": true,
	"grafana":         true,
	"ilogtail":        true,
	"alloy":           true,
	"originx-sdk-aut": true,
	"camera-agent":    true,
	"camera-apm-adap": true,
	"camera-receiver": true,
	"root-cause-infe": true,
	"tail":            true,
	"top":             true,
	"git":             true,
	"ssh":             true,
	"ps":              true,
	"vi":              true,
	"vim":             true,
	"tmux":            true,
}

func SkipBlackComm(comm string) bool {
	if comm == "" {
		return true
	}

	_, ok := blackComms[comm]
	return ok
}

func SkipBlackCmdline(cmdLine string) bool {
	if cmdLine == "" {
		return true
	}

	for _, black := range blackCmdList {
		if strings.HasPrefix(cmdLine, black) {
			return true
		}
	}
	return false
}
