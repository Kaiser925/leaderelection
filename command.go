// Copyright (c) 2023. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leaderelection

type HeartbeatRequest struct {
	Lead uint64
	Term uint64
}

type HeartbeatResponse struct {
	Success bool
	Term    uint64
}
