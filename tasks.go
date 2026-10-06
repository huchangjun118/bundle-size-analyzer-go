package analyzer

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// TaskStatus 任务状态。
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskDone      TaskStatus = "done"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// Progress 是某个任务的独立进度快照。
type Progress struct {
	ID      string
	Status  TaskStatus
	Current int64
	Total   int64
	Err     error
}

// TaskFunc 是任务体，report 用于上报进度（current, total）。
type TaskFunc func(ctx context.Context, report func(current, total int64)) error

// ErrQueueFull 在提交队列已满时返回，调用方应稍后重试而非无限堆积。
var ErrQueueFull = errors.New("task queue full")

// ErrClosed 在管理器已关闭后提交任务时返回。
var ErrClosed = errors.New("task manager closed")

type task struct {
	id     string
	fn     TaskFunc
	ctx    context.Context
	cancel context.CancelFunc
}

// TaskManager 以固定数量的 worker 并发执行任务。
// 队列有界：慢任务只会占满队列让后续提交收到 ErrQueueFull，
// 不会拖垮其他任务或造成内存无界增长；Close 后不留后台 goroutine。
type TaskManager struct {
	jobs   chan task
	wg     sync.WaitGroup // 跟踪 worker，Close 时等待全部退出
	mu     sync.Mutex
	states map[string]*taskState
	closed bool
}

type taskState struct {
	status  TaskStatus
	current int64
	total   int64
	err     error
	cancel  context.CancelFunc
}

// NewTaskManager 创建容量为 workers 的任务管理器，队列长度上限为 queueSize。
func NewTaskManager(workers, queueSize int) *TaskManager {
	if workers < 1 {
		workers = 1
	}
	if queueSize < 1 {
		queueSize = 1
	}
	tm := &TaskManager{
		jobs:   make(chan task, queueSize),
		states: make(map[string]*taskState),
	}
	tm.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go tm.worker()
	}
	return tm
}

func (tm *TaskManager) worker() {
	defer tm.wg.Done()
	for t := range tm.jobs {
		tm.run(t)
	}
}

func (tm *TaskManager) run(t task) {
	tm.mu.Lock()
	st := tm.states[t.id]
	if st.status != TaskPending {
		// 已在执行前被取消。
		tm.mu.Unlock()
		return
	}
	st.status = TaskRunning
	tm.mu.Unlock()

	report := func(current, total int64) {
		tm.mu.Lock()
		st.current, st.total = current, total
		tm.mu.Unlock()
	}

	err := t.fn(t.ctx, report)

	tm.mu.Lock()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			st.status = TaskCancelled
		} else {
			st.status = TaskFailed
		}
		st.err = err
	} else {
		st.status = TaskDone
	}
	tm.mu.Unlock()
}

// Submit 提交任务，队列已满时返回 ErrQueueFull 而非阻塞或无限堆积。
// 相同 id 重复提交返回错误。
func (tm *TaskManager) Submit(id string, fn TaskFunc) error {
	tm.mu.Lock()
	if tm.closed {
		tm.mu.Unlock()
		return ErrClosed
	}
	if _, dup := tm.states[id]; dup {
		tm.mu.Unlock()
		return fmt.Errorf("task %q already exists", id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	tm.states[id] = &taskState{status: TaskPending, cancel: cancel}
	tm.mu.Unlock()

	select {
	case tm.jobs <- task{id: id, fn: fn, ctx: ctx, cancel: cancel}:
		return nil
	default:
		tm.mu.Lock()
		delete(tm.states, id)
		tm.mu.Unlock()
		cancel()
		return ErrQueueFull
	}
}

// Cancel 取消指定任务：未开始的任务直接标记取消，运行中的任务收到取消信号。
func (tm *TaskManager) Cancel(id string) bool {
	tm.mu.Lock()
	st, ok := tm.states[id]
	if ok && st.status == TaskPending {
		st.status = TaskCancelled
	}
	tm.mu.Unlock()
	if ok {
		st.cancel()
	}
	return ok
}

// Progress 返回指定任务的进度快照。
func (tm *TaskManager) Progress(id string) (Progress, bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	st, ok := tm.states[id]
	if !ok {
		return Progress{}, false
	}
	return Progress{
		ID:      id,
		Status:  st.status,
		Current: st.current,
		Total:   st.total,
		Err:     st.err,
	}, true
}

// Close 停止接收新任务，等待已提交任务完成、全部 worker 退出后返回。
// 返回后不存在任何残留的后台 goroutine。
func (tm *TaskManager) Close() {
	tm.mu.Lock()
	if tm.closed {
		tm.mu.Unlock()
		return
	}
	tm.closed = true
	close(tm.jobs)
	tm.mu.Unlock()
	tm.wg.Wait()
}
