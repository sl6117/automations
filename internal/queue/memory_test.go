package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEnqueueIsIdempotent(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	job := Job{ID: "111#Sang", Payload: []byte("digest mee")}
	if err := q.Enqueue(ctx, "twitter-digest", job); err != nil {
		t.Fatal(err)
	}

	// simulate crash-and-rerun: same work enqueued again
	if err := q.Enqueue(ctx, "twitter-digest", job); err != nil {
		t.Fatal(err)
	}

	pending, err := q.Pending(ctx, "twitter-digest")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("want 1 pending job, got %d", len(pending))
	}

}

func TestEnqueueDoesNotResurectSettledJob(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	job := Job{ID: "111#Sang"}
	q.Enqueue(ctx, "twitter-digest", job)

	if ok, _ := q.Claim(ctx, "twitter-digest", job.ID, time.Minute); !ok {
		t.Fatal("first claim should succeed")
	}
	if err := q.Complete(ctx, "twitter-digest", job.ID); err != nil {
		t.Fatal(err)
	}

	// re-enqueue after delivery must NOT reset the job to pending
	// that'd double-send on the next drain
	q.Enqueue(ctx, "twitter-digest", job)
	pending, _ := q.Pending(ctx, "twitter-digest")
	if len(pending) != 0 {
		t.Fatalf("want 0 pending jobs, got %d", len(pending))
	}
}

func TestClaimIsExclusiveWhileLeaseIsLive(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()
	q.Enqueue(ctx, "twitter-digest", Job{ID: "111#Samg"})
	ok, err := q.Claim(ctx, "twitter-digest", "111#Samg", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	// a second worker racing for the same job must lose, without error
	ok, err = q.Claim(ctx, "twitter-digest", "111#Samg", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("second claim won while the first lease was live")
	}
}

func TestExpiredLeaseCanBeReclaimed(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()
	q.Enqueue(ctx, "twitter-digest", Job{ID: "111#Sang"})
	// a worker claims, then crashes: lease of -1s is already expired
	if ok, _ := q.Claim(ctx, "twitter-digest", "111#Sang", -time.Second); !ok {
		t.Fatal("first claim should succeed")
	}
	ok, err := q.Claim(ctx, "twitter-digest", "111#Sang", time.Minute)
	if err != nil || !ok {
		t.Fatalf("expired lease should be reclaimable: ok=%v err=%v", ok, err)
	}
}
func TestFailReleasesLeaseAndCountsAttempt(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()
	q.Enqueue(ctx, "twitter-digest", Job{ID: "111#Sang"})
	q.Claim(ctx, "twitter-digest", "111#Sang", time.Minute)
	if err := q.Fail(ctx, "twitter-digest", "111#Sang", errors.New("telegram 500"), false); err != nil {
		t.Fatal(err)
	}
	pending, _ := q.Pending(ctx, "twitter-digest")
	if len(pending) != 1 {
		t.Fatalf("non-final fail should leave job pending, got %d jobs", len(pending))
	}
	if pending[0].Attempts != 1 || pending[0].LastError != "telegram 500" {
		t.Fatalf("attempt not recorded: %+v", pending[0])
	}
	// the lease was released, so a retry can claim immediately
	if ok, _ := q.Claim(ctx, "twitter-digest", "111#Sang", time.Minute); !ok {
		t.Fatal("job should be claimable right after a non-final fail")
	}
}
func TestFinalFailStopsRetrying(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()
	q.Enqueue(ctx, "twitter-digest", Job{ID: "111#Sang"})
	q.Claim(ctx, "twitter-digest", "111#Sang", time.Minute)
	if err := q.Fail(ctx, "twitter-digest", "111#Sang", errors.New("gave up"), true); err != nil {
		t.Fatal(err)
	}
	pending, _ := q.Pending(ctx, "twitter-digest")
	if len(pending) != 0 {
		t.Fatal("failed (dead-letter) job must not show up as pending")
	}
	if ok, _ := q.Claim(ctx, "twitter-digest", "111#Sang", time.Minute); ok {
		t.Fatal("failed job must not be claimable")
	}
}
func TestQueuesAreIsolatedByName(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()
	q.Enqueue(ctx, "twitter-digest", Job{ID: "111#Sang"})
	q.Enqueue(ctx, "other-project", Job{ID: "222#Bob"})
	pending, _ := q.Pending(ctx, "twitter-digest")
	if len(pending) != 1 || pending[0].ID != "111#Sang" {
		t.Fatalf("queue isolation broken: %+v", pending)
	}
}
