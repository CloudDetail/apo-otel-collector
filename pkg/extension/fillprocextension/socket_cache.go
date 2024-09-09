package fillprocextension

import (
	"sync"

	"github.com/CloudDetail/apo-otel-collector/pkg/extension/fillprocextension/internal/proc"
	"go.uber.org/zap"
)

type SocketCache struct {
	logger        *zap.Logger
	Procs         map[int]*proc.ProcInfo // 无需加锁，只有定时扫描更新
	ToMapSockets  sync.Map               // <peerSocket, int>
	CachedSockets sync.Map               // <peerSocket, *ProcInfo
}

func newSocketCache(logger *zap.Logger) *SocketCache {
	return &SocketCache{
		logger: logger,
		Procs:  make(map[int]*proc.ProcInfo),
	}
}

func (c *SocketCache) mapSocketToPids() {
	toMatchPeers := make(map[string]int)
	c.ToMapSockets.Range(func(k, v interface{}) bool {
		toMatchPeers[k.(string)] = v.(int)
		// 清理待匹配peers
		c.ToMapSockets.Delete(k)
		return true
	})

	if len(toMatchPeers) > 0 {
		c.scanNewProcs()
		if len(c.Procs) == 0 {
			// Windows环境无法扫描进程信息，返回空.
			return
		}

		result := c.matchSockets(toMatchPeers)
		for peer, matchedProc := range result {
			if matchedProc != nil {
				c.logger.Info("Match Socket",
					zap.String("peer", peer),
					zap.Int("pid", matchedProc.ProcessID),
					zap.Int("nsPid", matchedProc.NsPid),
					zap.String("host", matchedProc.HostName),
					zap.String("Comm", matchedProc.Comm),
					zap.String("ContainerId", matchedProc.ContainerId),
				)
				c.CachedSockets.Store(peer, matchedProc)
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

	for pid := range c.Procs {
		if _, exist := pids[pid]; !exist {
			// 清理已删除PID
			delete(c.Procs, pid)
		}
	}

	for pid := range pids {
		if _, ok := c.Procs[pid]; !ok {
			// 新增PID
			process := proc.ScanProc(pid)
			c.Procs[pid] = process
			if !process.Ignore {
				c.logger.Info("Scan Proc",
					zap.Int("pid", process.ProcessID),
					zap.Int("nsPid", process.NsPid),
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
		for _, procInfo := range c.Procs {
			if procInfo.Ignore {
				// 考虑到容器应用也允许 开启主机模式，不对进程做过滤
				// 不分析Ignore
				continue
			}
			if matchPeer := procInfo.GetMatchNetSocket(socks); matchPeer != "" {
				result[matchPeer] = procInfo
				// 移除peers，减少后续检索
				delete(socks, matchPeer)
				delete(peers, matchPeer)
			}
			if len(socks) == 0 {
				break
			}
		}
	}

	if len(peers) == 0 {
		return result
	}

	for _, procInfo := range c.Procs {
		if procInfo.Ignore || procInfo.IsVm() {
			// 不分析Ignore 和 虚机场景
			continue
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
			return result
		}
	}

	if len(peers) == 0 {
		return result
	}

	for peer, serverPort := range peers {
		c.logger.Warn("NotMatch Socket",
			zap.String("peer", peer),
			zap.Int("port", serverPort),
		)
	}
	return result
}