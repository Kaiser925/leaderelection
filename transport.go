// Copyright (c) 2023. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leaderelection

import (
	"context"
)

// HeartBeatMsg is the message sent by the leader to the followers.
type HeartBeatMsg struct {
	Lead uint64 // Current leader announcing itself.
	Term uint64 // Current term.
}

// VoteRequest is the message sent by the candidate to the followers.
type VoteRequest struct {
	Candidate uint64 // Candidate requesting vote.
	Term      uint64 // Candidate's term.
}

// VoteResponse is the response send by the follower to the candidate.
type VoteResponse struct {
	VoterID uint64
	Term    uint64 // Voter term.
	Granted bool   // True means candidate received vote.
	Reason  string // Used for debugging.
}

type RPCResponse struct {
	Response any
	Error    error
}

type RPC struct {
	Command  any
	RespChan chan<- RPCResponse
}

// Respond is used to respond with a response, error or both
func (r *RPC) Respond(resp any, err error) {
	r.RespChan <- RPCResponse{resp, err}
}

// Transport provides an interface for network transports
// to allow node to communicate with other nodes.
type Transport interface {
	// SendVoteRequest sends a vote request message to the given peer.
	SendVoteRequest(ctx context.Context, peer string, msg *VoteRequest, resp *VoteResponse) error
	// SendHeartbeat sends a heartbeat message to the given peer.
	SendHeartbeat(ctx context.Context, peer string, msg *HeartBeatMsg, resp *HeartbeatResponse) error
	// Consumer returns a channel that can be used to
	// consume and respond to RPC requests.
	Consumer() <-chan RPC
}
