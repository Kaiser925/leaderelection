// Copyright (c) 2023. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leaderelection

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
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
	RespChan chan RPCResponse
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

// HTTP2Transport implements Transport with http2.
type HTTP2Transport struct {
	rpcC chan RPC

	cli    *http.Client
	server *http.Server
}

// NewHTTP2Transport returns a new HTTP2Transport instance.
func NewHTTP2Transport(addr string) *HTTP2Transport {
	trans := &HTTP2Transport{}

	mux := http.NewServeMux()
	mux.HandleFunc("/heartbeat", trans.handleHeartbeat)
	mux.HandleFunc("/vote", trans.handleVote)

	trans.server = &http.Server{
		Addr:    addr,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

	trans.cli = &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
				//  TODO(zhang): implements tls
				return net.Dial(network, addr)
			},
		},
	}

	return trans
}

func (h *HTTP2Transport) ListenAndServe() error {
	return h.server.ListenAndServe()
}

func (h *HTTP2Transport) Shutdown(ctx context.Context) error {
	return h.server.Shutdown(ctx)
}

func (h *HTTP2Transport) SendVoteRequest(ctx context.Context, peer string,
	msg *VoteRequest, resp *VoteResponse) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}

	res, err := h.cli.Post(fmt.Sprintf("http://%s/heartbeat", peer),
		"application/json", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer res.Body.Close()

	if err := json.NewDecoder(res.Body).Decode(resp); err != nil {
		return fmt.Errorf("json decode: %w", err)
	}

	return nil
}

func (h *HTTP2Transport) SendHeartbeat(ctx context.Context, peer string,
	msg *HeartBeatMsg, resp *HeartbeatResponse) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}

	res, err := h.cli.Post(fmt.Sprintf("http://%s/heartbeat", peer),
		"application/json", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer res.Body.Close()

	if err := json.NewDecoder(res.Body).Decode(resp); err != nil {
		return fmt.Errorf("json decode: %w", err)
	}
	return nil
}

func (h *HTTP2Transport) Consumer() <-chan RPC {
	return h.rpcC
}

func (h *HTTP2Transport) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	req := &HeartbeatRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
	}

	rpc := RPC{Command: req, RespChan: make(chan RPCResponse)}
	h.rpcC <- rpc
	resp := <-rpc.RespChan
	if resp.Error != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(resp.Error.Error()))
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp.Response.(HeartbeatResponse))
}

func (h *HTTP2Transport) handleVote(w http.ResponseWriter, r *http.Request) {
	req := &VoteRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
	}

	rpc := RPC{Command: req, RespChan: make(chan RPCResponse)}
	h.rpcC <- rpc
	resp := <-rpc.RespChan
	if resp.Error != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(resp.Error.Error()))
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp.Response.(VoteResponse))
}
