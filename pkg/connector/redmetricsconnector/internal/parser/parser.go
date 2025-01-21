package parser

import (
	"fmt"
	"os"
	"strconv"

	"github.com/CloudDetail/apo-otel-collector/pkg/connector/redmetricsconnector/internal/cache"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

const (
	Unknown = "unknown"

	envNodeName = "MY_NODE_NAME"
	envNodeIP   = "MY_NODE_IP"

	AttributeNetPeerName   = "net.peer.name"  // 1.x
	AttributeNetPeerPort   = "net.peer.port"  // 1.x
	AttributeServerAddress = "server.address" // 2.x
	AttributeServerPort    = "server.port"    // 2.x

	AttributeNetSockPeerAddr    = "net.sock.peer.addr"   // 1.x
	AttributeNetSockPeerPort    = "net.sock.peer.port"   // 1.x
	AttributeNetworkPeerAddress = "network.peer.address" // 2.x
	AttributeNetworkPeerPort    = "network.peer.port"    // 2.x

	keyServiceName = "svc_name"
	keyContentKey  = "content_key"
	keyNodeName    = "node_name"
	keyNodeIp      = "node_ip"
	keyTopSpan     = "top_span"
	keyIsError     = "is_error"
	keyPid         = "pid"
	keyContainerId = "container_id"

	keyName     = "name"
	keyDbSystem = "db_system"
	keyDbName   = "db_name"
	keyDbUrl    = "db_url"
	keyAddress  = "address"
	keySystem   = "system"
	keyRole     = "role"
)

var (
	NodeName = getNodeName()
	NodeIp   = getNodeIp()
)

type Parser interface {
	Parse(logger *zap.Logger, pid string, containerId string, serviceName string, entryUrl string, span *ptrace.Span, spanAttr pcommon.Map, keyValue *cache.ReusedKeyValue) string
}

func BuildServerKey(keyValue *cache.ReusedKeyValue, pid string, containerId string, serviceName string, name string, topSpan bool, isError bool) string {
	keyValue.Reset()
	keyValue.
		Add(keyServiceName, serviceName).
		Add(keyContentKey, name).
		Add(keyNodeName, NodeName).
		Add(keyNodeIp, NodeIp).
		Add(keyPid, pid).
		Add(keyContainerId, containerId).
		Add(keyTopSpan, strconv.FormatBool(topSpan)).
		Add(keyIsError, strconv.FormatBool(isError))
	return keyValue.GetValue()
}

// buildExternalKey Http/Rpc
func BuildExternalKey(keyValue *cache.ReusedKeyValue, pid string, containerId string, serviceName string, entryUrl string, name string, peer string, system string, isError bool) string {
	keyValue.Reset()
	keyValue.
		Add(keyServiceName, serviceName).
		Add(keyContentKey, entryUrl).
		Add(keyNodeName, NodeName).
		Add(keyNodeIp, NodeIp).
		Add(keyPid, pid).
		Add(keyContainerId, containerId).
		Add(keyName, name).
		Add(keyAddress, peer).
		Add(keySystem, system).
		Add(keyIsError, strconv.FormatBool(isError))
	return keyValue.GetValue()
}

// buildDbKey DB
func buildDbKey(keyValue *cache.ReusedKeyValue, pid string, containerId string, serviceName string, entryUrl string, dbSystem string, dbName string, name string, dbUrl string, isError bool) string {
	keyValue.Reset()
	keyValue.
		Add(keyServiceName, serviceName).
		Add(keyContentKey, entryUrl).
		Add(keyNodeName, NodeName).
		Add(keyNodeIp, NodeIp).
		Add(keyPid, pid).
		Add(keyContainerId, containerId).
		Add(keyName, name).
		Add(keyDbSystem, dbSystem).
		Add(keyDbName, dbName).
		Add(keyDbUrl, dbUrl).
		Add(keyIsError, strconv.FormatBool(isError))
	return keyValue.GetValue()
}

// buildMqKey ActiveMq / RabbitMq / RocketMq / Kafka
func buildMqKey(keyValue *cache.ReusedKeyValue, pid string, containerId string, serviceName string, entryUrl string, name string, peer string, mqSystem string, isError bool, role string) string {
	keyValue.Reset()
	keyValue.
		Add(keyServiceName, serviceName).
		Add(keyContentKey, entryUrl).
		Add(keyNodeName, NodeName).
		Add(keyNodeIp, NodeIp).
		Add(keyPid, pid).
		Add(keyContainerId, containerId).
		Add(keyName, name).
		Add(keyAddress, peer).
		Add(keySystem, mqSystem).
		Add(keyRole, role).
		Add(keyIsError, strconv.FormatBool(isError))
	return keyValue.GetValue()
}

func GetClientPeer(attr pcommon.Map) string {
	// 1.x - redis、grpc、rabbitmq
	if netSockPeerAddr, addrFound := attr.Get(AttributeNetSockPeerAddr); addrFound {
		if netSockPeerPort, portFound := attr.Get(AttributeNetSockPeerPort); portFound {
			return fmt.Sprintf("%s:%s", netSockPeerAddr.Str(), netSockPeerPort.AsString())
		} else {
			return netSockPeerAddr.Str()
		}
	}
	// 2.x - redis、grpc、rabbitmq
	if networkPeerAddress, addrFound := attr.Get(AttributeNetworkPeerAddress); addrFound {
		if networkPeerPort, portFound := attr.Get(AttributeNetworkPeerPort); portFound {
			return fmt.Sprintf("%s:%s", networkPeerAddress.Str(), networkPeerPort.AsString())
		} else {
			return networkPeerAddress.Str()
		}
	}

	// 1.x - httpclient、db、dubbo
	if peerName, peerFound := attr.Get(AttributeNetPeerName); peerFound {
		if peerPort, peerPortFound := attr.Get(AttributeNetPeerPort); peerPortFound {
			return fmt.Sprintf("%s:%s", peerName.Str(), peerPort.AsString())
		} else {
			return peerName.Str()
		}
	}
	// 2.x - httpclient、db、dubbo
	if serverAddress, serverFound := attr.Get(AttributeServerAddress); serverFound {
		if serverPort, serverPortFound := attr.Get(AttributeServerPort); serverPortFound {
			return fmt.Sprintf("%s:%s", serverAddress.Str(), serverPort.AsString())
		} else {
			return serverAddress.Str()
		}
	}

	return ""
}

func getNodeName() string {
	if nodeNameFromEnv, exist := os.LookupEnv(envNodeName); exist {
		return nodeNameFromEnv
	}
	if hostName, err := os.Hostname(); err == nil {
		return hostName
	}
	return "Unknown"
}

func getNodeIp() string {
	if nodeIpFromEnv, exist := os.LookupEnv(envNodeIP); exist {
		return nodeIpFromEnv
	}
	return "Unknown"
}

func getAttrValueWithDefault(attr pcommon.Map, key string, defaultValue string) string {
	// The more specific span attribute should take precedence.
	attrValue, exists := attr.Get(key)
	if exists {
		return attrValue.AsString()
	}
	return defaultValue
}
