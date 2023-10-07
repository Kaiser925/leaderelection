package leaderelection

type HeartBeatRequest struct {
	Lead uint64
	Term uint64
}

type HeartBeatResponse struct {
	Success bool
}
