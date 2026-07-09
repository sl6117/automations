package queue

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Memory is an in-process queue for unit tests. It mirrors the semantics the DDB BE enforces with conditional writes
type Memory struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func NewMemory() *Memory {
	return &Memory{jobs: make(map[string]*Job)}
}

func (m *Memory) Enqueue(ctx context.Context, name string, job Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := name + "#" + job.ID

	if _, exists := m.jobs[key]; exists {
		return nil
	}
	job.Status = StatusPending
	job.CreatedAt = time.Now().UTC()
	m.jobs[key] = &job
	return nil
}

func (m *Memory) Pending(ctx context.Context, name string) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []Job
	for key, job := range m.jobs {
		if key == name+"#"+job.ID && job.Status == StatusPending {
			out = append(out, *job)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (m *Memory) Claim(ctx context.Context, name, id string, lease time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[name+"#"+id]
	if !ok || job.Status != StatusPending || time.Now().Before(job.LeaseUntil) {
		return false, nil
	}
	job.LeaseUntil = time.Now().Add(lease)
	return true, nil
}

func (m *Memory) Complete(ctx context.Context, name, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[name+"#"+id]
	if !ok {
		return fmt.Errorf("complete %s#%s: job not found", name, id)
	}
	job.Status = StatusDelivered
	return nil
}

func (m *Memory) Fail(ctx context.Context, name, id string, jobErr error, final bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[name+"#"+id]
	if !ok {
		return fmt.Errorf("fail %s#%s: job not found", name, id)
	}
	job.Attempts++
	job.LastError = jobErr.Error()
	job.LeaseUntil = time.Time{}
	if final {
		job.Status = StatusFailed
	}
	return nil
}
