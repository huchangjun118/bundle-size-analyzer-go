package analyzer

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTasksRunConcurrentlyWithIndependentProgress(t *testing.T) {
	tm := NewTaskManager(4, 16)
	defer tm.Close()

	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(4)
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("t%d", i)
		if err := tm.Submit(id, func(ctx context.Context, report func(c, t int64)) error {
			started.Done()
			report(1, 2)
			<-release // 全部任务同时挂起，证明并发执行
			report(2, 2)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	started.Wait() // 若 worker 不足导致串行，这里会死锁超时

	// 各自维护独立进度。
	for i := 0; i < 4; i++ {
		p, ok := tm.Progress(fmt.Sprintf("t%d", i))
		if !ok || p.Status != TaskRunning || p.Current != 1 || p.Total != 2 {
			t.Fatalf("progress %d = %+v", i, p)
		}
	}
	close(release)
	tm.Close()
	for i := 0; i < 4; i++ {
		p, _ := tm.Progress(fmt.Sprintf("t%d", i))
		if p.Status != TaskDone {
			t.Fatalf("task %d status = %s", i, p.Status)
		}
	}
}

func TestSlowTaskDoesNotBlockOthers(t *testing.T) {
	tm := NewTaskManager(2, 8)
	defer tm.Close()

	slow := make(chan struct{})
	if err := tm.Submit("slow", func(ctx context.Context, report func(c, t int64)) error {
		<-slow
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var fastDone atomic.Int32
	for i := 0; i < 4; i++ {
		if err := tm.Submit(fmt.Sprintf("fast%d", i), func(ctx context.Context, report func(c, t int64)) error {
			fastDone.Add(1)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for fastDone.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fastDone.Load() != 4 {
		t.Fatalf("fast tasks blocked by slow task: %d/4 done", fastDone.Load())
	}
	close(slow)
}

func TestQueueFullAndNoLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	tm := NewTaskManager(1, 2)

	block := make(chan struct{})
	tm.Submit("running", func(ctx context.Context, report func(c, t int64)) error {
		<-block
		return nil
	})
	tm.Submit("queued1", func(ctx context.Context, report func(c, t int64)) error { return nil })
	tm.Submit("queued2", func(ctx context.Context, report func(c, t int64)) error { return nil })
	// 队列已满：必须返回错误而非无限堆积。
	if err := tm.Submit("overflow", func(ctx context.Context, report func(c, t int64)) error { return nil }); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("want ErrQueueFull, got %v", err)
	}
	// 重复 ID 被拒绝。
	if err := tm.Submit("queued1", func(ctx context.Context, report func(c, t int64)) error { return nil }); err == nil {
		t.Fatal("want duplicate id error")
	}

	close(block)
	tm.Close()
	// Close 后提交被拒绝。
	if err := tm.Submit("late", func(ctx context.Context, report func(c, t int64)) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("want ErrClosed, got %v", err)
	}
	// 后台 goroutine 不泄漏。
	time.Sleep(50 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before+1 {
		t.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}

func TestCancelAndFailure(t *testing.T) {
	tm := NewTaskManager(2, 8)
	defer tm.Close()

	tm.Submit("fail", func(ctx context.Context, report func(c, t int64)) error {
		return errors.New("boom")
	})
	tm.Submit("cancel", func(ctx context.Context, report func(c, t int64)) error {
		<-ctx.Done()
		return ctx.Err()
	})
	time.Sleep(50 * time.Millisecond)
	if !tm.Cancel("cancel") {
		t.Fatal("cancel should find the task")
	}
	tm.Close()

	p, _ := tm.Progress("fail")
	if p.Status != TaskFailed || p.Err == nil {
		t.Fatalf("fail task = %+v", p)
	}
	p, _ = tm.Progress("cancel")
	if p.Status != TaskCancelled {
		t.Fatalf("cancel task = %+v", p)
	}
	if _, ok := tm.Progress("nonexistent"); ok {
		t.Fatal("unknown task should not be found")
	}
}
