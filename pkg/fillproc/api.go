package fillproc

import "context"

type FillProc interface {
	GetMatchPidAndContainerId(ctx context.Context) (int, string)
}
