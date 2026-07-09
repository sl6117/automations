// Package queue provides durable delivery jobs: enqueue once, claim with a lease, retry on failure
// the contract is at-least-once delivery
// a job may occasionally be delivered twice, but is never silently lost
package queue

import (
	"context"
	"time"
)

// Status ist the job lifecycle: pending -> delivered on success, or pending -> failed
// once the caller gives up
type Status string

const (
	StatusPending   Status = "pending"
	StatusDelivered Status = "delivered"
	StatusFailed    Status = "failed"
)

// Job is one unit of delivery work: this payload, to this recipient
// ID must be deterministic for the same work (e.g. "newestTweetID#Subscriber")
type Job struct {
	ID         string
	Payload    []byte
	Status     Status
	Attempts   int
	LeaseUntil time.Time
	LastError  string
	CreatedAt  time.Time
}

// Queue is the contract every backend satisfies. Th ename parameter
// namespaces jobs per project (e.g. "twitter-digest")
type Queue interface {
	// Enqueue creates a job in pending state. If a job with the same ID
	// already exists, Enqueue does nothing (idempotent create).
	Enqueue(ctx context.Context, name string, job Job) error
	// Pending returns all jobs still in pending state, oldest first.
	Pending(ctx context.Context, name string) ([]Job, error)
	// Claim leases a pending job for the given duration. Returns false if
	// the job is not pending or another worker holds a live lease.
	Claim(ctx context.Context, name, id string, lease time.Duration) (bool, error)
	// Complete marks a claimed job delivered.
	Complete(ctx context.Context, name, id string) error
	// Fail records one failed attempt: increments the counter, stores the
	// error, releases the lease. If final is true the job moves to failed
	// and stops retrying — that's the dead-letter state.
	Fail(ctx context.Context, name, id string, jobErr error, final bool) error
}
