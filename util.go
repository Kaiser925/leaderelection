package leaderelection

import (
	"math/rand"
	"time"
)

// RandomTimeout returns a channel that will fire after a random duration.
// The duration is between minVal and 2*minVal.
func RandomTimeout(minVal time.Duration) <-chan time.Time {
	if minVal == 0 {
		return nil
	}
	extra := time.Duration(rand.Int63()) % minVal
	return time.After(minVal + extra)
}
