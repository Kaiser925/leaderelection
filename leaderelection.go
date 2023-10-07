package leaderelection

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

type Node struct {
	electionState

	id        uint64
	peerMu    sync.RWMutex
	peers     map[uint64]string
	transport Transport
	lead      uint64

	errorc       chan error
	stopc        chan struct{}
	peerChangedC chan struct{}
	rpcC         chan RPC

	heartbeatTimeout time.Duration
	heartbeatC       chan HeartBeatMsg
}

func NewNode(id uint64, peers map[uint64]string) *Node {
	return &Node{
		id:               id,
		peers:            peers,
		heartbeatTimeout: randomTimeout(time.Second),

		stopc:        make(chan struct{}),
		peerChangedC: make(chan struct{}),
		errorc:       make(chan error),
	}
}

func randomTimeout(minVal time.Duration) time.Duration {
	if minVal == 0 {
		return 0
	}
	extra := time.Duration(rand.Int63()) % minVal
	return minVal + extra
}

func (n *Node) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			close(n.stopc)
			return ctx.Err()
		case err := <-n.errorc:
			return err
		default:
		}

		switch n.getState() {
		case Follower:
			n.runFollower()
		case Candidate:
			n.runCandidate()
		case Leader:
			n.runLeader()
		}
	}
}

func (n *Node) runFollower() {
	heartbeatTimer := time.NewTimer(n.heartbeatTimeout)
	defer heartbeatTimer.Stop()

	for n.getState() == Follower {
		select {
		case msg := <-n.heartbeatC:
			heartbeatTimer.Reset(n.heartbeatTimeout)
			if msg.Term >= n.getCurrentTerm() {
				n.setCurrentTerm(msg.Term)
			}
			if msg.Lead != n.getLead() {
				n.setLead(msg.Lead)
			}
		case rpc := <-n.rpcC:
			n.processRPC(rpc)
		case <-heartbeatTimer.C:
			n.setState(Candidate)
		case <-n.stopc:
			return
		}
	}
}

func (n *Node) runCandidate() {
	log.Printf("Node %d is running for election", n.id)
	n.setCurrentTerm(n.getCurrentTerm() + 1)
	for n.getState() == Candidate {
		// start vote
		select {
		case msg := <-n.heartbeatC:
			// has new leader
			if msg.Term > n.getCurrentTerm() {
				n.setState(Follower)
				n.setCurrentTerm(msg.Term)
				n.setLead(msg.Lead)
			}
		case <-n.stopc:
			return
		}

	}
}

func (n *Node) runLeader() {
	// start heartbeat goroutine
	n.heartbeat()
	for n.getState() == Leader {
		select {
		case msg := <-n.heartbeatC:
			// term is out of date
			if msg.Term > n.getCurrentTerm() {
				n.setState(Follower)
				n.setCurrentTerm(msg.Term)
				n.setLead(msg.Lead)
			}
		case <-n.stopc:
			return
		}
	}
}

func (n *Node) getLead() uint64 {
	return atomic.LoadUint64(&n.lead)
}

func (n *Node) setLead(lead uint64) {
	atomic.StoreUint64(&n.lead, lead)
}

func (n *Node) IsLeader() bool {
	return n.getState() == Leader
}

func (n *Node) AddPeer(id uint64, url string) {
	n.peerMu.Lock()
	defer n.peerMu.Unlock()
	n.peers[id] = url
	n.peerChangedC <- struct{}{}
}

func (n *Node) RemovePeer(id uint64) {
	n.peerMu.Lock()
	defer n.peerMu.Unlock()
	delete(n.peers, id)
	n.peerChangedC <- struct{}{}
}

func (n *Node) processRPC(rpc RPC) {
	switch cmd := rpc.Command.(type) {
	case *HeartBeatRequest:
		n.processHeartBeat(rpc, cmd)
	default:
		rpc.Respond(nil, errors.New("unknown command"))
	}
}

func (n *Node) processHeartBeat(rpc RPC, req *HeartBeatRequest) {
	resp := &HeartBeatResponse{Success: true}
	if req.Term < n.getCurrentTerm() {
		resp.Success = false
		rpc.Respond(resp, nil)
		return
	}
	rpc.Respond(resp, nil)
}

func (n *Node) heartbeat() {

}
