package leaderelection

type HeartBeatMsg struct {
	Lead uint64
	Term uint64
}

// Transport is the interface that wraps node communication.
type Transport interface {
	Run() error
	Stop()
}
