// Package testutil holds helpers shared by the tests of several packages
// (the counterpart of the Python tests' conftest.py).
package testutil

import "time"

// WaitUntil polls condition until it is true, for up to 5 s; false on timeout.
//
// Used where a background goroutine does the work, instead of fixed sleeps:
// quick when the machine is fast, still passing when it is slow.
func WaitUntil(condition func() bool) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
