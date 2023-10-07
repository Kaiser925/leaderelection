package leaderelection

import "sync/atomic"

// State is used to represent the state of the leader election
type State uint32

const (
	// Follower is initial state of the leader election
	Follower State = iota
	// Candidate is one of the valid states of the leader election
	Candidate
	// Leader is one of the valid states of the leader election
	Leader
)

func (s State) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return "Unknown"
	}
}

type electionState struct {
	// current state
	state State
	// currentTerm is the latest term the server has seen
	currentTerm uint64
	// votedTerm is the term the server voted for
	votedTerm uint64
}

func (s *electionState) getState() State {
	ptr := (*uint32)(&s.state)
	return State(atomic.LoadUint32(ptr))
}

func (s *electionState) setState(state State) {
	ptr := (*uint32)(&s.state)
	atomic.StoreUint32(ptr, uint32(state))
}

func (s *electionState) getCurrentTerm() uint64 {
	return atomic.LoadUint64(&s.currentTerm)
}

func (s *electionState) setCurrentTerm(term uint64) {
	atomic.StoreUint64(&s.currentTerm, term)
}

func (s *electionState) getVotedTerm() uint64 {
	return atomic.LoadUint64(&s.votedTerm)
}

func (s *electionState) setVotedTerm(votedTerm uint64) {
	atomic.StoreUint64(&s.votedTerm, votedTerm)
}
