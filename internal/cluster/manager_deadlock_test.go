package cluster

import (
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/lb"
)

// TestManagerDeadlockReproduction 复现死锁问题
// 问题: applyTargets/rebuildFromBalancer 在持有写锁时调用 persist(),
// 而 persist() 内部调用 CurrentTable() 尝试获取读锁，导致死锁
func TestManagerDeadlockReproduction(t *testing.T) {
	targets := []lb.Target{
		{ID: "a", Addr: "http://a:9000", Weight: 1, Healthy: true},
		{ID: "b", Addr: "http://b:9000", Weight: 1, Healthy: true},
	}

	// 不使用 cacheDir，避免异步 persist goroutine 在 Windows 上
	// 导致 TempDir 清理时文件被占用
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(targets)
	mgr := NewManager(logger, balancer, targets, "")

	// 使用channel检测死锁
	done := make(chan bool, 1)

	go func() {
		// 触发 MarkUnhealthy，内部会调用 rebuildFromBalancer
		// 如果存在死锁，此行会阻塞
		mgr.MarkUnhealthy("a")
		done <- true
	}()

	// 等待操作完成或超时
	select {
	case <-done:
		// 测试通过，没有死锁
		t.Log("MarkUnhealthy completed without deadlock")
	case <-time.After(2 * time.Second):
		// 超时，说明发生了死锁
		t.Fatal("DEADLOCK DETECTED: MarkUnhealthy blocked for 2 seconds")
	}
}

// TestManagerDeadlockWithCacheEnabled 在启用缓存目录的情况下测试死锁
func TestManagerDeadlockWithCacheEnabled(t *testing.T) {
	targets := []lb.Target{
		{ID: "x", Addr: "http://x:9000", Weight: 1, Healthy: true},
	}

	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(targets)
	mgr := NewManager(logger, balancer, targets, dir)

	// 测试多个并发操作
	var wg sync.WaitGroup
	errors := make(chan string, 10)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(iteration int) {
			defer wg.Done()

			done := make(chan bool, 1)
			go func() {
				if iteration%2 == 0 {
					mgr.MarkUnhealthy("x")
				} else {
					mgr.MarkHealthy("x")
				}
				done <- true
			}()

			select {
			case <-done:
				// 成功
			case <-time.After(2 * time.Second):
				errors <- "deadlock detected in iteration"
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	errCount := 0
	for err := range errors {
		t.Logf("Error: %s", err)
		errCount++
	}

	if errCount > 0 {
		t.Fatalf("Detected %d deadlock(s)", errCount)
	}

	// 等待异步 persist goroutine 完成文件写入，避免 Windows 上
	// TempDir 清理时因文件句柄占用导致 RemoveAll 失败
	time.Sleep(100 * time.Millisecond)
}

// TestManagerDeadlockUpdateTargets 测试 UpdateTargets 是否也会导致死锁
func TestManagerDeadlockUpdateTargets(t *testing.T) {
	targets := []lb.Target{
		{ID: "old", Addr: "http://old:9000", Weight: 1, Healthy: true},
	}

	// 不使用 cacheDir，避免异步 persist goroutine 在 Windows 上
	// 导致 TempDir 清理时文件被占用
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(targets)
	mgr := NewManager(logger, balancer, targets, "")

	done := make(chan bool, 1)

	go func() {
		newTargets := []lb.Target{
			{ID: "new", Addr: "http://new:9000", Weight: 1, Healthy: true},
		}
		mgr.UpdateTargets(newTargets)
		done <- true
	}()

	select {
	case <-done:
		t.Log("UpdateTargets completed without deadlock")
	case <-time.After(2 * time.Second):
		t.Fatal("DEADLOCK DETECTED: UpdateTargets blocked for 2 seconds")
	}
}

// TestManagerConcurrentReadWrite 测试并发读写是否会触发死锁
func TestManagerConcurrentReadWrite(t *testing.T) {
	targets := []lb.Target{
		{ID: "a", Addr: "http://a:9000", Weight: 1, Healthy: true},
		{ID: "b", Addr: "http://b:9000", Weight: 1, Healthy: true},
	}

	// 不使用 cacheDir，避免异步 persist goroutine 在 Windows 上
	// 导致 TempDir 清理时文件被占用
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(targets)
	mgr := NewManager(logger, balancer, targets, "")

	// 启动多个goroutine进行读写
	var wg sync.WaitGroup
	stop := make(chan bool)

	// 写操作goroutine
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if id%2 == 0 {
						mgr.MarkUnhealthy("a")
					} else {
						mgr.MarkHealthy("a")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	// 读操作goroutine
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = mgr.CurrentTable()
					time.Sleep(5 * time.Millisecond)
				}
			}
		}()
	}

	// 运行一段时间
	time.Sleep(500 * time.Millisecond)
	close(stop)

	// 使用带超时的Wait
	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		t.Log("All goroutines completed without deadlock")
	case <-time.After(3 * time.Second):
		t.Fatal("DEADLOCK DETECTED: goroutines blocked during concurrent operations")
	}
}
