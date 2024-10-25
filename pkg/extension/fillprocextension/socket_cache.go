package fillprocextension

import (
	"fmt"
	"sync"

	"github.com/CloudDetail/apo-otel-collector/pkg/extension/fillprocextension/internal/proc"
	"go.uber.org/zap"
)

type SocketCache struct {
	logger           *zap.Logger
	activeProcNum    int
	Procs            sync.Map // <int, *proc.ProcInfo>
	ToMapSockets     sync.Map // <peerSocket, *ToMatchData>
	CachedSockets    sync.Map // <peerSocket, *ProcInfo
	CachedProcs      sync.Map // <service-instanceId, *ProcInfo>
	HostNetNamespace string
}

func newSocketCache(logger *zap.Logger) *SocketCache {
	return &SocketCache{
		logger:        logger,
		activeProcNum: 0,
	}
}

func (c *SocketCache) mapSocketToPids() {
	toMatchPeers := make(map[string]int)
	toRelateInstances := make([]*ToMatchData, 0)
	c.ToMapSockets.Range(func(k, v interface{}) bool {
		toMatchData := v.(*ToMatchData)
		toMatchPeers[k.(string)] = toMatchData.Port
		toRelateInstances = append(toRelateInstances, toMatchData)
		// 清理待匹配peers
		c.ToMapSockets.Delete(k)
		return true
	})

	if len(toMatchPeers) > 0 {
		c.scanNewProcs()
		if c.activeProcNum == 0 {
			// Windows环境无法扫描进程信息，返回空.
			return
		}
		if c.HostNetNamespace == "" {
			if hostProc, exist := c.Procs.Load(1); exist {
				c.HostNetNamespace = hostProc.(*proc.ProcInfo).NetNamespace
			}
		}

		result := c.matchSockets(toMatchPeers)
		for _, toRelate := range toRelateInstances {
			if matchedProc, found := result[toRelate.Peer]; found {
				c.logger.Info("Match Socket",
					zap.String("peer", toRelate.Peer),
					zap.Int("pid", matchedProc.ProcessID),
					zap.String("host", matchedProc.HostName),
					zap.String("Comm", matchedProc.Comm),
					zap.String("ContainerId", matchedProc.ContainerId),
					zap.String("service", toRelate.Service),
					zap.String("instanceId", toRelate.InstanceId),
				)
				c.CachedSockets.Store(toRelate.Peer, matchedProc)
				c.RelateServiceInstance(toRelate.Service, toRelate.InstanceId, matchedProc)
			} else {
				c.logger.Warn("NotMatch Socket",
					zap.String("peer", toRelate.Peer),
					zap.Int("port", toRelate.Port),
					zap.String("service", toRelate.Service),
					zap.String("instanceId", toRelate.InstanceId),
				)
			}
		}
	}
}

func (c *SocketCache) scanNewProcs() {
	pids, err := proc.FindAllProcesses()
	if err != nil {
		c.logger.Error("Scan Process Failed", zap.Error(err))
		return
	}

	c.Procs.Range(func(k, v interface{}) bool {
		if _, exist := pids[k.(int)]; !exist {
			// 清理已删除PID
			c.Procs.Delete(k)
			c.activeProcNum -= 1
		}
		return true
	})

	for pid := range pids {
		if _, ok := c.Procs.Load(pid); !ok {
			// 新增PID
			process := proc.ScanProc(pid)
			c.Procs.Store(pid, process)
			c.activeProcNum += 1
			if !process.Ignore {
				c.logger.Info("Scan Proc",
					zap.Int("pid", process.ProcessID),
					zap.String("host", process.HostName),
					zap.String("Comm", process.Comm),
					zap.String("ContainerId", process.ContainerId),
				)
			}
		}
	}
}

func (c *SocketCache) matchSockets(peers map[string]int) map[string]*proc.ProcInfo {
	result := make(map[string]*proc.ProcInfo)
	// 先分析检索VM的Proc，VM应用由于共用同一个Net，需相关遍历/proc/{pid}/fd文件夹
	if socks, err := proc.ListVMMatchNetSocks(peers); err == nil && len(socks) > 0 {
		// 基于Peer寻找到对应的Sock
		c.Procs.Range(func(k, v interface{}) bool {
			procInfo := v.(*proc.ProcInfo)
			if procInfo.Ignore {
				// 考虑到容器应用也允许 开启主机模式，不对进程做过滤
				// 不分析Ignore
				return true
			}
			if matchPeer := procInfo.GetMatchNetSocket(socks); matchPeer != "" {
				result[matchPeer] = procInfo
				// 移除peers，减少后续检索
				delete(socks, matchPeer)
				delete(peers, matchPeer)
			}
			if len(socks) == 0 {
				return false
			}
			return true
		})
	}

	if len(peers) == 0 {
		return result
	}

	if c.HostNetNamespace != "" {
		c.Procs.Range(func(k, v interface{}) bool {
			procInfo := v.(*proc.ProcInfo)
			if procInfo.Ignore || procInfo.NetNamespace == c.HostNetNamespace {
				// 不分析Ignore 和 虚机场景
				return true
			}
			// 容器网络场景，分析每个PID的tcp数据
			if socks, err := procInfo.ListMatchNetSocks(peers); err == nil && len(socks) > 0 {
				for peer := range socks {
					result[peer] = procInfo
					// 移除peers，减少后续检索
					delete(peers, peer)
				}
			}

			if len(peers) == 0 {
				// 已完全匹配，提前退出
				return false
			}
			return true
		})
	}
	return result
}

func (c *SocketCache) GetContainerIdByPid(pid int) string {
	if procInfoInterface, found := c.Procs.Load(pid); found {
		return procInfoInterface.(*proc.ProcInfo).ContainerId
	}
	return ""
}

func (c *SocketCache) GetProcByServiceInstance(service string, instanceId string) *proc.ProcInfo {
	if service == "" || instanceId == "" {
		return nil
	}
	key := fmt.Sprintf("%s-%s", service, instanceId)
	if procInterface, ok := c.CachedProcs.Load(key); ok {
		return procInterface.(*proc.ProcInfo)
	}
	return nil
}

func (c *SocketCache) RelateServiceInstance(service string, instanceId string, procInfo *proc.ProcInfo) {
	if service == "" || instanceId == "" {
		c.logger.Warn("Ignore Relate Service Instance",
			zap.String("service", service),
			zap.String("instanceId", instanceId),
		)
		return
	}
	key := fmt.Sprintf("%s-%s", service, instanceId)
	if procInterface, ok := c.CachedProcs.Load(key); ok {
		existProc := procInterface.(*proc.ProcInfo)

		c.logger.Warn("Skip Relate Service Instance",
			zap.String("service", service),
			zap.String("instanceId", instanceId),
			zap.Int("pid", procInfo.ProcessID),
			zap.String("containerId", procInfo.ContainerId),
			zap.Int("stored.pid", existProc.ProcessID),
			zap.String("stored.containerId", existProc.ContainerId),
		)
	} else {
		c.CachedProcs.Store(key, procInfo)

		c.logger.Warn("Relate Service Instance",
			zap.String("service", service),
			zap.String("instanceId", instanceId),
			zap.Int("pid", procInfo.ProcessID),
			zap.String("containerId", procInfo.ContainerId),
		)
	}
}

type ToMatchData struct {
	Peer       string
	Port       int
	Service    string
	InstanceId string
}
