//go:build windows

package main

import "os"

// notifySIGHUP は Windows では SIGHUP が利用できないため、何もしません。
// On Windows, SIGHUP is not available; hot-reload via signal is a no-op.
// Use 'sproxy start' with a new process to reload configuration on Windows.
func notifySIGHUP(ch chan<- os.Signal) {
	// no-op on Windows
}
