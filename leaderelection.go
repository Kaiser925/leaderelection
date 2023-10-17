// Copyright (c) 2023. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leaderelection

import (
	"context"
	"errors"
	"log/slog"
	"maps"
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
	peerChangedC chan uint64
	rpcC         <-chan RPC

	// lastContact is the last time we had contact from the leader
	lastContact   time.Time
	lastContactMu sync.RWMutex

	heartbeatTimeout time.Duration
	electionTimeout  time.Duration
}

// NewNode returns new Node.
func NewNode(id uint64, peers map[uint64]string, transport Transport) *Node {
	return &Node{
		id:               id,
		peers:            peers,
		heartbeatTimeout: randomTimeout(time.Second),

		stopc:        make(chan struct{}),
		peerChangedC: make(chan uint64),
		errorc:       make(chan error),

		transport: transport,
		rpcC:      transport.Consumer(),
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
		case <-n.stopc:
			return nil
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
	voteResC := n.electSelf()

	granted := 0
	votesNeeded := n.quorumSize()

	electionTimeout := RandomTimeout(n.electionTimeout)

	for n.getState() == Candidate {
		select {
		case rpc := <-n.rpcC:
			n.processRPC(rpc)
		case res := <-voteResC:
			if res.Term > n.getCurrentTerm() {
				slog.Info("term out of date", "node", n.id, "currentTerm", n.getCurrentTerm(), "resTerm", res.Term)
				n.setCurrentTerm(res.Term)
				n.setState(Follower)
				return
			}

			if res.Granted {
				granted++
				slog.Info("vote granted", "node", n.id, "voter", res.VoterID, "granted", granted)
			}

			if granted >= votesNeeded {
				slog.Info("become leader", "node", n.id)
				n.setState(Leader)
				n.setLead(n.id)
				return
			}
		case <-electionTimeout:
			slog.Info("election timeout", "node", n.id)
			return
		case <-n.stopc:
			return
		}

	}
}

func (n *Node) runLeader() {
	stopCs := make(map[uint64]chan struct{}, len(n.getPeers()))
	defer func() {
		for _, stopC := range stopCs {
			close(stopC)
		}
	}()

	for id, addr := range n.getPeers() {
		if id == n.id {
			continue
		}
		stopC := make(chan struct{})
		go n.heartbeat(addr, stopC)
		stopCs[id] = stopC
	}

	for n.getState() == Leader {
		select {
		case rpc := <-n.rpcC:
			n.processRPC(rpc)
		case id := <-n.peerChangedC:
			stopc, ok := stopCs[id]
			if ok {
				close(stopc)
				delete(stopCs, n.id)
			} else {
				stopC := make(chan struct{})
				addr := n.getPeers()[id]
				go n.heartbeat(addr, stopC)
				stopCs[id] = stopC
			}
		case <-n.stopc:
			return
		}
	}
}

func (n *Node) electSelf() <-chan *VoteResponse {
	respC := make(chan *VoteResponse, len(n.getPeers()))
	req := &VoteRequest{
		Candidate: n.id,
		Term:      n.getCurrentTerm(),
	}

	for id, addr := range n.getPeers() {
		if id == n.id {
			respC <- &VoteResponse{
				VoterID: n.id,
				Granted: true,
				Term:    req.Term,
			}
		} else {
			go func(addr string) {
				resp := &VoteResponse{}
				ctx, cancel := context.WithTimeout(context.Background(), n.electionTimeout)
				defer cancel()
				err := n.transport.SendVoteRequest(ctx, addr, req, resp)
				if err != nil {
					resp.Granted = false
					resp.Term = req.Term
				}
				respC <- resp
			}(addr)
		}
	}
	return respC
}

func (n *Node) getPeers() map[uint64]string {
	n.peerMu.RLock()
	defer n.peerMu.RUnlock()
	res := make(map[uint64]string, len(n.peers))
	maps.Copy(n.peers, res)
	return res
}

func (n *Node) quorumSize() int {
	return len(n.getPeers())/2 + 1
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
	n.peerChangedC <- id
}

func (n *Node) RemovePeer(id uint64) {
	n.peerMu.Lock()
	defer n.peerMu.Unlock()
	delete(n.peers, id)
	n.peerChangedC <- id
}

func (n *Node) processRPC(rpc RPC) {
	switch cmd := rpc.Command.(type) {
	case *HeartbeatRequest:
		n.processHeartBeat(rpc, cmd)
	case *VoteRequest:
		n.processVoteRequest(rpc, cmd)
	default:
		rpc.Respond(nil, errors.New("unknown command"))
	}
}

func (n *Node) processHeartBeat(rpc RPC, req *HeartbeatRequest) {
	resp := &HeartbeatResponse{Term: n.getCurrentTerm()}
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
	resp := &VoteResponse{VoterID: n.id, Term: n.getCurrentTerm()}
	var respErr error
	defer func() {
		rpc.Respond(resp, respErr)
	}()

	if req.Term < n.getCurrentTerm() {
		slog.Info("rejecting vote request", "node", n.id, "reqTerm", req.Term, "currentTerm", n.getCurrentTerm())
		return
	}

	if req.Term > n.getCurrentTerm() {
		slog.Info("get newer", "node", n.id, "reqTerm", req.Term, "currentTerm", n.getCurrentTerm())
		n.setState(Follower)
		n.setCurrentTerm(req.Term)
		resp.Term = req.Term
	}

	if req.Term <= n.getVotedTerm() {
		slog.Info("rejecting vote request, already voted", "node", n.id, "reqTerm", req.Term, "votedTerm", n.getVotedTerm())
		return
	}

	slog.Info("voting for", "node", n.id, "candidate", req.Candidate, "reqTerm", req.Term)
	resp.Granted = true
	n.setVotedTerm(req.Term)
	n.setCurrentTerm(req.Term)
	n.setLastContact()
}

func (n *Node) heartbeat(addr string, stopc chan struct{}) {
	for {
		select {
		case <-RandomTimeout(n.heartbeatTimeout / 10):
			req := &HeartBeatMsg{
				Lead: n.id,
				Term: n.getCurrentTerm(),
			}
			resp := HeartbeatResponse{}
			err := n.transport.SendHeartbeat(context.Background(), addr, req, &resp)
			if err != nil {
				slog.Error("send heartbeat failed", "node", n.id, "addr", addr, "err", err)
			}
		case <-stopc:
			return
		}
	}
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
