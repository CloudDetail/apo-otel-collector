package fillproc

import "context"

type FillProc interface {
	GetMatchPidAndContainerId(ctx context.Context, serviceName string, instanceId string, sdkName string) (int, string)

	GetMatchPidAndContainerIdForHttp(peerAddr string, serverAddr string, serviceName string, instanceId string, sdkName string) (int, string)
}
