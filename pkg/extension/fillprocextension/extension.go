package fillprocextension

type FillProc interface {
	GetMatchPidAndContainerId(peer string, port int) (int, string)
}
