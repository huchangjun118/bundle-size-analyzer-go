package tasks

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitFor(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", msg)
}

func TestIndependentProgress(t *testing.T) {
	m := New(4)
	defer m.Close()
	const n = 8
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("task-%d", i)
		total := int64((i + 1) * 10)
		err := m.Submit(id, func(ctx context.Context, report Reporter) error {
			for j := int64(1); j <= total; j++ {
				report(j, total)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}
	waitFor(t, 5*time.Second, func() bool {
		for i := 0; i < n; i++ {
			p, ok := m.Progress(fmt.Sprintf("task-%d", i))
			if !ok || p.Status != StatusDone {
				return false
			}
		}
		return true
	}, "all tasks done")

	// 各自进度独立且完整
	for i := 0; i < n; i++ {
		p, _ := m.Progress(fmt.Sprintf("task-%d", i))
		want := int64((i + 1) * 10)
		if p.Completed != want || p.Total != want {
			t.Fatalf("task-%d progress = %+v, want %d/%d", i, p, want, want)
		}
	}
}

func TestSlowTaskDoesNotBlockOthers(t *testing.T) {
	m := New(2)
	defer m.Close()

	slowStarted := make(chan struct{})
	slowRelease := make(chan struct{})
	defer close(slowRelease)

	if err := m.Submit("slow", func(ctx context.Context, report Reporter) error {
		close(slowStarted)
		select {
		case <-slowRelease:
		case <-ctx.Done():
		}
		return nil
	}); err != nil {
		t.Fatalf("Submit slow: %v", err)
	}
	<-slowStarted

	// 慢任务占用一个额度时，另一个任务仍能完成
	if err := m.Submit("fast", func(ctx context.Context, report Reporter) error {
		report(1, 1)
		return nil
	}); err != nil {
		t.Fatalf("Submit fast: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool {
		p, ok := m.Progress("fast")
		return ok && p.Status == StatusDone
	}, "fast task completes while slow task runs")
}

func TestConcurrencyLimit(t *testing.T) {
	m := New(2)
	defer m.Close()
	var running, maxRunning int64
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := m.Submit(fmt.Sprintf("t-%d", i), func(ctx context.Context, report Reporter) error {
				cur := atomic.AddInt64(&running, 1)
				for {
					old := atomic.LoadInt64(&maxRunning)
					if cur <= old || atomic.CompareAndSwapInt64(&maxRunning, old, cur) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				atomic.AddInt64(&running, -1)
				return nil
			})
			if err != nil {
				t.Errorf("Submit: %v", err)
			}
		}(i)
	}
	wg.Wait()
	m.Close()
	if got := atomic.LoadInt64(&maxRunning); got > 2 {
		t.Fatalf("max concurrent = %d, want <= 2", got)
	}
}

func TestFailureAndCancel(t *testing.T) {
	m := New(2)
	defer m.Close()

	if err := m.Submit("boom", func(ctx context.Context, report Reporter) error {
		return fmt.Errorf("kaboom")
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool {
		p, ok := m.Progress("boom")
		return ok && p.Status == StatusFailed && p.Err == "kaboom"
	}, "failed task records error")

	block := make(chan struct{})
	if err := m.Submit("victim", func(ctx context.Context, report Reporter) error {
		<-ctx.Done()
		return nil
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	close(block)
	m.Cancel("victim")
	waitFor(t, 5*time.Second, func() bool {
		p, ok := m.Progress("victim")
		return ok && p.Status == StatusCanceled
	}, "canceled task reaches canceled status")
}

func TestSubmitValidation(t *testing.T) {
	m := New(1)
	if err := m.Submit("x", nil); err == nil {
		t.Fatal("want error for nil task")
	}
	if err := m.Submit("x", func(ctx context.Context, r Reporter) error { return nil }); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if err := m.Submit("x", func(ctx context.Context, r Reporter) error { return nil }); err == nil {
		t.Fatal("want error for duplicate id")
	}
	m.Close()
	if err := m.Submit("y", func(ctx context.Context, r Reporter) error { return nil }); err == nil {
		t.Fatal("want error after Close")
	}
}

// 已结束任务记录有保留上限，长期运行不会内存无界增长。
func TestFinishedRetentionBound(t *testing.T) {
	m := New(4)
	m.maxFinished = 16
	for i := 0; i < 200; i++ {
		id := fmt.Sprintf("t-%d", i)
		if err := m.Submit(id, func(ctx context.Context, r Reporter) error { return nil }); err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}
	m.Close()
	m.mu.Lock()
	n := len(m.tasks)
	m.mu.Unlock()
	if n > 16 {
		t.Fatalf("retained finished tasks = %d, want <= 16", n)
	}
}

// Close 后不应有 goroutine 残留：用完成计数验证所有任务都退出。
func TestNoLeakAfterClose(t *testing.T) {
	m := New(3)
	var done int64
	for i := 0; i < 50; i++ {
		if err := m.Submit(fmt.Sprintf("t-%d", i), func(ctx context.Context, r Reporter) error {
			time.Sleep(time.Millisecond)
			atomic.AddInt64(&done, 1)
			return nil
		}); err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}
	m.Close() // Close 返回即代表全部任务 goroutine 已退出
	if got := atomic.LoadInt64(&done); got != 50 {
		t.Fatalf("completed = %d, want 50", got)
	}
}
