// Package tasks 提供并发任务管理。
//
// 多个分析/对比任务并发运行，各自维护独立进度；并发度由信号量上限控制，
// 单个慢任务只会占用一个额度，不会阻塞其他任务的提交与调度。
// 已结束任务的记录有保留上限（FIFO 淘汰），长时间运行不会内存无界增长；
// Close 会等待全部任务退出，不产生后台 goroutine 泄漏。
package tasks

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Status 是任务状态。
type Status string

const (
	StatusPending  Status = "pending"
	StatusRunning  Status = "running"
	StatusDone     Status = "done"
	StatusFailed   Status = "failed"
	StatusCanceled Status = "canceled"
)

// Progress 是任务的进度快照。
type Progress struct {
	Status    Status `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
	Err       string `json:"err,omitempty"`
}

// Reporter 由任务函数调用以上报进度。
type Reporter func(completed, total int64)

// TaskFunc 是任务体。ctx 取消时任务应尽快返回。
type TaskFunc func(ctx context.Context, report Reporter) error

// 已结束任务记录的默认保留上限。
const defaultRetainedFinished = 1024

type taskState struct {
	progress Progress
	cancel   context.CancelFunc
	finished bool
}

// Manager 管理并发任务。零值不可用，请用 New 创建。
type Manager struct {
	sem         chan struct{}
	mu          sync.Mutex
	tasks       map[string]*taskState
	finishedSeq []string // FIFO，用于淘汰最老的已结束记录
	maxFinished int
	wg          sync.WaitGroup
	closed      bool
}

// New 创建任务管理器。maxConcurrent 为最大并发任务数，<=0 时取 1。
func New(maxConcurrent int) *Manager {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	return &Manager{
		sem:         make(chan struct{}, maxConcurrent),
		tasks:       map[string]*taskState{},
		maxFinished: defaultRetainedFinished,
	}
}

// Submit 提交任务。id 必须唯一；管理器已关闭或 id 重复时返回错误。
// 任务在独立 goroutine 中运行，本调用立即返回。
func (m *Manager) Submit(id string, fn TaskFunc) error {
	if fn == nil {
		return errors.New("tasks: nil task function")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("tasks: manager closed")
	}
	if _, exists := m.tasks[id]; exists {
		m.mu.Unlock()
		return fmt.Errorf("tasks: duplicate task id %q", id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	st := &taskState{progress: Progress{Status: StatusPending}, cancel: cancel}
	m.tasks[id] = st
	m.wg.Add(1)
	m.mu.Unlock()

	go m.run(id, st, ctx, fn)
	return nil
}

func (m *Manager) run(id string, st *taskState, ctx context.Context, fn TaskFunc) {
	defer m.wg.Done()
	defer st.cancel() // 释放 ctx 资源，防泄漏

	m.sem <- struct{}{}        // 获取并发额度；慢任务只占一个额度
	defer func() { <-m.sem }() // 保证释放，不泄漏额度

	m.mu.Lock()
	if ctx.Err() != nil {
		m.finishLocked(id, st, Progress{Status: StatusCanceled})
		m.mu.Unlock()
		return
	}
	st.progress.Status = StatusRunning
	m.mu.Unlock()

	report := func(completed, total int64) {
		m.mu.Lock()
		st.progress.Completed = completed
		st.progress.Total = total
		m.mu.Unlock()
	}

	err := fn(ctx, report)

	p := Progress{Completed: st.progress.Completed, Total: st.progress.Total}
	switch {
	case ctx.Err() != nil:
		p.Status = StatusCanceled
	case err != nil:
		p.Status = StatusFailed
		p.Err = err.Error()
	default:
		p.Status = StatusDone
	}
	m.mu.Lock()
	m.finishLocked(id, st, p)
	m.mu.Unlock()
}

// finishLocked 记录终态并执行保留上限淘汰。调用方须持有 m.mu。
func (m *Manager) finishLocked(id string, st *taskState, p Progress) {
	st.progress = p
	st.finished = true
	m.finishedSeq = append(m.finishedSeq, id)
	for len(m.finishedSeq) > m.maxFinished {
		evict := m.finishedSeq[0]
		m.finishedSeq = m.finishedSeq[1:]
		delete(m.tasks, evict)
	}
}

// Progress 返回指定任务的进度快照。
func (m *Manager) Progress(id string) (Progress, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.tasks[id]
	if !ok {
		return Progress{}, false
	}
	return st.progress, true
}

// Cancel 请求取消任务；任务终态为 canceled。
func (m *Manager) Cancel(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.tasks[id]; ok && !st.finished {
		st.cancel()
	}
}

// Close 拒绝新提交并等待全部任务退出。可多次调用。
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.wg.Wait()
}
