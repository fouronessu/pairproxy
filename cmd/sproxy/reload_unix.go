//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// notifySIGHUP 在 Unix/Linux/macOS 上注册 SIGHUP 信号监听。
// SIGHUP 通常由 systemd reload 或 `kill -HUP <pid>` 触发，用于配置热重载。
func notifySIGHUP(ch chan<- os.Signal) {
	signal.Notify(ch, syscall.SIGHUP)
}
