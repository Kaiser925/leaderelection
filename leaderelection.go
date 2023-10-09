// Copyright (c) 2023. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leaderelection

import (
	"errors"
	"log/slog"
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

	// lastContact is the last time we had contact from the leader
	lastContact   time.Time
	lastContactMu sync.RWMutex

	heartbeatTimeout time.Duration
}

// NewNode returns new Node.
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

func (n *Node) Run() error {
	for {
		select {
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

func (n *Node) Stop() {
	close(n.stopc)
}

func (n *Node) runFollower() {
	heartbeatTimer := RandomTimeout(n.heartbeatTimeout)

	for n.getState() == Follower {
		select {
		case rpc := <-n.rpcC:
			n.processRPC(rpc)
		case <-heartbeatTimer:
			lastContact := n.getLastContact()
			// restart heartbeat timer
			heartbeatTimer = RandomTimeout(n.heartbeatTimeout)
			if time.Since(lastContact) < n.heartbeatTimeout {
				continue
			}
			slog.Info("heartbeat timeout", "node", n.id, "lastContact", lastContact)
			n.setState(Candidate)
		case <-n.stopc:
			return
		}
	}
}

func (n *Node) runCandidate() {
	slog.Info("start election", "node", n.id)
	n.setCurrentTerm(n.getCurrentTerm() + 1)
	for n.getState() == Candidate {
		select {
		case rpc := <-n.rpcC:
			n.processRPC(rpc)
		case <-n.stopc:
			return
		}

	}
}

func (n *Node) runLeader() {
	for n.getState() == Leader {
		select {
		case rpc := <-n.rpcC:
			n.processRPC(rpc)
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
	case *VoteRequest:
		n.processVoteRequest(rpc, cmd)
	default:
		rpc.Respond(nil, errors.New("unknown command"))
	}
}

func (n *Node) processHeartBeat(rpc RPC, req *HeartBeatRequest) {
	resp := &HeartBeatResponse{Term: n.getCurrentTerm()}
	if req.Term < n.getCurrentTerm() {
		resp.Success = false
		rpc.Respond(resp, nil)
		return
	}

	if req.Term > n.getCurrentTerm() {
		n.setCurrentTerm(req.Term)
		n.setLead(req.Lead)
	}

	resp.Success = true
	rpc.Respond(resp, nil)
}

func (n *Node) processVoteRequest(rpc RPC, req *VoteRequest) {

}

func (n *Node) heartbeat() {

}

func (n *Node) getLastContact() time.Time {
	n.lastContactMu.RLock()
	defer n.lastContactMu.RUnlock()
	return n.lastContact
}

func (n *Node) setLastContact() {
	n.lastContactMu.Lock()
	defer n.lastContactMu.Unlock()
	n.lastContact = time.Now()
}
