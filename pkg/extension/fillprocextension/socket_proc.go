package fillprocextension

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/CloudDetail/apo-otel-collector/pkg/common/timeutils"
	"github.com/CloudDetail/apo-otel-collector/pkg/extension/fillprocextension/internal/proc"
	"go.uber.org/zap"
	"google.golang.org/grpc/peer"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
)

type SocketProcExtension struct {
	logger *zap.Logger
	cache  *SocketCache

	enable                bool
	interval              time.Duration
	checkSocketProcTicker timeutils.TTicker
}

func newSocketProcExtension(settings extension.Settings, config component.Config) *SocketProcExtension {
	cfg := config.(*Config)

	interval := cfg.Interval
	if interval.Microseconds() < 1000 {
		// 不允许设置1秒以下的定时间隔.
		interval = defaultSocketProcInterval
	}

	enable := cfg.Enable
	if enable {
		// Windows环境没有访问/proc权限，现只支持Linux.
		enable = proc.AccessProc()
	}
	socketProc := &SocketProcExtension{
		logger:   settings.Logger,
		enable:   enable,
		interval: interval,
	}

	if socketProc.enable {
		cache := newSocketCache(settings.Logger)
		socketProc.cache = cache
		socketProc.checkSocketProcTicker = &timeutils.PolicyTicker{OnTickFunc: cache.mapSocketToPids}
	}
	return socketProc
}

func (p *SocketProcExtension) Start(context.Context, component.Host) error {
	p.logger.Info("Start socketProc Extension", zap.Bool("enable", p.enable))
	if p.enable {
		p.checkSocketProcTicker.Start(p.interval)
	}
	return nil
}

func (p *SocketProcExtension) Shutdown(context.Context) error {
	if p.enable {
		p.checkSocketProcTicker.Stop()
	}
	return nil
}

func (p *SocketProcExtension) GetMatchPidAndContainerId(ctx context.Context, serviceName string, instanceId string, sdkName string) (int, string) {
	if !p.enable {
		return 0, ""
	}
	peerAddr, ok := peer.FromContext(ctx)
	if !ok {
		return 0, ""
	}
	return p.getMatchPidAndContainerId(peerAddr.Addr.String(), peerAddr.LocalAddr.String(), serviceName, instanceId, sdkName)
}

func (p *SocketProcExtension) GetMatchPidAndContainerIdForHttp(peerAddr string, serverAddr string, serviceName string, instanceId string, sdkName string) (int, string) {
	if !p.enable || len(peerAddr) == 0 || len(serverAddr) == 0 {
		return 0, ""
	}
	return p.getMatchPidAndContainerId(peerAddr, serverAddr, serviceName, instanceId, sdkName)
}

func (p *SocketProcExtension) getMatchPidAndContainerId(peerAddr string, serverAddr string, serviceName string, instanceId string, sdkName string) (int, string) {
	if sdkName == "beyla" {
		if pid, err := strconv.Atoi(instanceId[strings.LastIndex(instanceId, "-")+1:]); err == nil {
			return pid, p.cache.GetContainerIdByPid(pid)
		}
	}

	if matchedProc := p.cache.GetProcByServiceInstance(serviceName, instanceId); matchedProc != nil {
		return matchedProc.ProcessID, matchedProc.ContainerId
	}

	peer := peerAddr
	if strings.HasPrefix(peer, "[::1]:") {
		peer = fmt.Sprintf("::1:%s", peer[6:])
	}

	// 获取已关联Proc
	if procInterface, found := p.cache.CachedSockets.Load(peer); found {
		procInfo := procInterface.(*proc.ProcInfo)
		return procInfo.ProcessID, procInfo.ContainerId
	}

	// 判断是否已存储待关联Peer
	if _, exist := p.cache.ToMapSockets.Load(peer); !exist {
		port, _ := strconv.Atoi(serverAddr[strings.LastIndex(serverAddr, ":")+1:])
		// 记录Peer到待关联列表
		p.cache.ToMapSockets.Store(peer, &ToMatchData{
			Peer:       peer,
			Port:       port,
			Service:    serviceName,
			InstanceId: instanceId,
		})
	}
	return 0, ""
}
