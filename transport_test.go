// Copyright (c) 2023. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leaderelection

import (
	"context"
	"testing"
	"time"
)

func TestHTTP2Transport_SendHeartbeat(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h2 := NewHTTP2Transport("127.0.0.1:12345")

	go h2.ListenAndServe()
	defer h2.Shutdown(context.TODO())
	resp := HeartbeatResponse{}

	go func() {
		err := h2.SendHeartbeat(ctx, "127.0.0.1:12345", &HeartBeatMsg{1, 1}, &resp)
		if err != nil {
			t.Errorf("send heartbeat failed: %v", err)
			return
		}
	}()

	for {
		select {
		case rpc := <-h2.rpcC:
			rpc.Respond(HeartbeatResponse{Success: true, Term: 1}, nil)
		case <-time.After(2 * time.Second):
			return
		}
	}

	if !resp.Success {
		t.Errorf("should be sucess")
	}
}
