package proc

import (
	"bytes"
	"os"
	"path"
	"strconv"
	"strings"
)

const (
	PATH_ROOT     = "/proc"
	PREFIX_SOCKET = "socket:["
)

type ProcInfo struct {
	ProcessID    int
	ExeName      string
	CmdLine      string
	Comm         string
	HostName     string
	ContainerId  string
	NetNamespace string
	Ignore       bool
}

func ScanProc(pid int) (p *ProcInfo) {
	p = &ProcInfo{
		ProcessID: pid,
	}
	// 存在Exe不存在
	if p.ExeName, _ = GetExeName(pid); p.ExeName == "" {
		p.Ignore = true
		return
	}
	// 提取获取NetNameSpace，避免被黑名单过滤
	if p.NetNamespace, _ = GetNetNamespace(pid); p.NetNamespace == "" {
		p.Ignore = true
		return
	}
	// 过滤黑名单Comm
	if p.Comm, _ = GetComm(pid); SkipBlackComm(p.Comm) {
		p.Ignore = true
		return
	}
	// 过滤黑名单Cmdline
	if p.CmdLine, _ = GetCommandLine(pid); SkipBlackCmdline(p.CmdLine) {
		p.Ignore = true
		return
	}
	// 存在进程不存在/etc/hostname文件，eg. pause
	if p.HostName, _ = GetHostName(pid); p.HostName == "" {
		p.Ignore = true
		return
	}
	// 读取ContainerId
	p.ContainerId, _ = GetContainerIDFromCGroup(pid)

	return
}

func (p *ProcInfo) ListMatchNetSocks(peers map[string]int) (map[string]*SockTabEntry, error) {
	result := make(map[string]*SockTabEntry, 0)
	if err := DoNetstat(Path(p.ProcessID, "net", "tcp"), peers, result); err != nil {
		return nil, err
	}

	if err := DoNetstat(Path(p.ProcessID, "net", "tcp6"), peers, result); err != nil {
		return nil, err
	}

	return result, nil
}

func (p *ProcInfo) GetMatchNetSocket(socks map[string]*SockTabEntry) string {
	dirs, err := os.ReadDir(Path(p.ProcessID, "fd"))
	if err != nil {
		return ""
	}

	for _, di := range dirs {
		if di.IsDir() {
			continue
		}
		lname, err := os.Readlink(Path(p.ProcessID, "fd", di.Name()))
		if err != nil || !strings.HasPrefix(lname, PREFIX_SOCKET) {
			continue
		}
		for peer, sock := range socks {
			ss := PREFIX_SOCKET + sock.Ino + "]"
			if ss == lname {
				return peer
			}
		}
	}
	return ""
}

func ListVMMatchNetSocks(peers map[string]int) (map[string]*SockTabEntry, error) {
	result := make(map[string]*SockTabEntry, 0)
	if err := DoNetstat("/proc/net/tcp", peers, result); err != nil {
		return nil, err
	}
	if err := DoNetstat("/proc/net/tcp6", peers, result); err != nil {
		return nil, err
	}

	return result, nil
}

// The exe Symbolic Link: Inside each process's directory in /proc,
// there is a symbolic link named exe. This link points to the executable
// file that was used to start the process.
// For instance, if a process was started from /usr/bin/python,
// the exe symbolic link in that process's /proc directory will point to /usr/bin/python.
func GetExeName(pid int) (string, error) {
	return os.Readlink(Path(pid, "exe"))
}

// reads the command line arguments of a Linux process from
// the cmdline file in the /proc filesystem and converts them into a string
func GetCommandLine(pid int) (string, error) {
	fileContent, err := os.ReadFile(Path(pid, "cmdline"))
	if err != nil {
		return "", err
	} else {
		// \u0000替换为空格
		newByte := bytes.ReplaceAll([]byte(fileContent), []byte{0}, []byte{32})
		newByte = bytes.TrimSpace(newByte)
		return string(newByte), nil
	}
}

func GetComm(pid int) (string, error) {
	fileContent, err := os.ReadFile(Path(pid, "comm"))
	if err != nil {
		return "", err
	}
	// 移除换行符
	return strings.TrimSuffix(string(fileContent), "\n"), nil
}

func GetHostName(pid int) (string, error) {
	hostName, err := os.ReadFile(Path(pid, "root", "etc", "hostname"))
	if err != nil {
		return "", err
	} else {
		return strings.TrimSuffix(string(hostName), "\n"), nil
	}
}

func GetNetNamespace(pid int) (string, error) {
	return os.Readlink(Path(pid, "ns", "net"))
}

func Path(pid int, subpath ...string) string {
	return path.Join(append([]string{PATH_ROOT, strconv.Itoa(pid)}, subpath...)...)
}

func FindAllProcesses() (map[int]bool, error) {
	dirs, err := os.ReadDir(PATH_ROOT)
	if err != nil {
		return nil, err
	}
	result := map[int]bool{}
	for _, di := range dirs {
		if !di.IsDir() {
			continue
		}
		if pid, _ := isDirectoryPid(di.Name()); pid > 0 {
			result[pid] = true
		}
	}
	return result, nil
}

func isDirectoryPid(procDirectoryName string) (int, error) {
	if procDirectoryName[0] < '0' || procDirectoryName[0] > '9' {
		return 0, nil
	}
	return strconv.Atoi(procDirectoryName)
}

func AccessProc() bool {
	_, err := os.Stat(PATH_ROOT)
	return err == nil
}
