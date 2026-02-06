//go:build envtest
// +build envtest

package envtest

import (
	"testing"
	"time"
)

func waitForCondition(t *testing.T, timeout, interval time.Duration, check func() (bool, string), onTimeout func(last string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		ok, msg := check()
		last = msg
		if ok {
			return
		}
		time.Sleep(interval)
	}
	if onTimeout != nil {
		onTimeout(last)
	}
	t.Fatalf("condition not met within %s: %s", timeout, last)
}
