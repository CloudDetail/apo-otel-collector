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
	"runsvdir":        true,
	"runsv":           true,
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
