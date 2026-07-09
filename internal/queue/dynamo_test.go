//go:build integration

package queue

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func newTestDynamo(t *testing.T) *Dynamo {
	t.Helper()
	d, err := NewDynamo(context.Background())
	if err != nil {
		t.Fatalf("new dynamo: %v", err)
	}
	return d
}

// testQueue returns a unique test/-prefixed queue name and registers a
// cleanup sweep of every job item under its pk, so test data never
// accumulates in the real table
func testQueue(t *testing.T, d *Dynamo) string {
	t.Helper()
	name := fmt.Sprintf("test#delivery-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		ctx := context.Background()
		out, err := d.client.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(d.table),
			KeyConditionExpression: aws.String("pk = :pk"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": &types.AttributeValueMemberS{Value: pkOf(name)},
			},
		})
		if err != nil {
			t.Logf("cleanup query %q: %v", name, err)
			return
		}
		for _, item := range out.Items {
			if _, err := d.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
				TableName: aws.String(d.table),
				Key:       map[string]types.AttributeValue{"pk": item["pk"], "sk": item["sk"]},
			}); err != nil {
				t.Logf("cleanup delete %q: %v", name, err)
			}
		}
	})
	return name
}
func TestDynamoEnqueueIsIdempotentAndRoundTrips(t *testing.T) {
	d := newTestDynamo(t)
	ctx := context.Background()
	name := testQueue(t, d)
	job := Job{ID: "111#Sang", Payload: []byte("digest for Sang")}
	if err := d.Enqueue(ctx, name, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := d.Enqueue(ctx, name, job); err != nil {
		t.Fatalf("re-enqueue: %v", err)
	}
	pending, err := d.Pending(ctx, name)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("want 1 pending job, got %d", len(pending))
	}
	got := pending[0]
	if got.ID != "111#Sang" || string(got.Payload) != "digest for Sang" ||
		got.Status != StatusPending || got.Attempts != 0 {
		t.Fatalf("job did not round-trip: %+v", got)
	}
}
func TestDynamoEnqueueDoesNotResurrectSettledJob(t *testing.T) {
	d := newTestDynamo(t)
	ctx := context.Background()
	name := testQueue(t, d)
	job := Job{ID: "111#Sang"}
	if err := d.Enqueue(ctx, name, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if ok, err := d.Claim(ctx, name, job.ID, time.Minute); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := d.Complete(ctx, name, job.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := d.Enqueue(ctx, name, job); err != nil {
		t.Fatalf("re-enqueue: %v", err)
	}
	pending, err := d.Pending(ctx, name)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("delivered job came back as pending: %+v", pending)
	}
}
func TestDynamoClaimIsExclusiveWhileLeaseIsLive(t *testing.T) {
	d := newTestDynamo(t)
	ctx := context.Background()
	name := testQueue(t, d)
	if err := d.Enqueue(ctx, name, Job{ID: "111#Sang"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	ok, err := d.Claim(ctx, name, "111#Sang", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	ok, err = d.Claim(ctx, name, "111#Sang", time.Minute)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if ok {
		t.Fatal("second claim won while the first lease was live")
	}
}
func TestDynamoExpiredLeaseCanBeReclaimed(t *testing.T) {
	d := newTestDynamo(t)
	ctx := context.Background()
	name := testQueue(t, d)
	if err := d.Enqueue(ctx, name, Job{ID: "111#Sang"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if ok, _ := d.Claim(ctx, name, "111#Sang", -time.Second); !ok {
		t.Fatal("first claim should succeed")
	}
	ok, err := d.Claim(ctx, name, "111#Sang", time.Minute)
	if err != nil || !ok {
		t.Fatalf("expired lease should be reclaimable: ok=%v err=%v", ok, err)
	}
}
func TestDynamoFailReleasesLeaseAndCountsAttempt(t *testing.T) {
	d := newTestDynamo(t)
	ctx := context.Background()
	name := testQueue(t, d)
	if err := d.Enqueue(ctx, name, Job{ID: "111#Sang"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if ok, _ := d.Claim(ctx, name, "111#Sang", time.Minute); !ok {
		t.Fatal("claim should succeed")
	}
	if err := d.Fail(ctx, name, "111#Sang", errors.New("telegram 500"), false); err != nil {
		t.Fatalf("fail: %v", err)
	}
	pending, err := d.Pending(ctx, name)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("non-final fail should leave job pending, got %d jobs", len(pending))
	}
	if pending[0].Attempts != 1 || pending[0].LastError != "telegram 500" {
		t.Fatalf("attempt not recorded: %+v", pending[0])
	}
	if ok, _ := d.Claim(ctx, name, "111#Sang", time.Minute); !ok {
		t.Fatal("job should be claimable right after a non-final fail")
	}
}
func TestDynamoFinalFailStopsRetrying(t *testing.T) {
	d := newTestDynamo(t)
	ctx := context.Background()
	name := testQueue(t, d)
	if err := d.Enqueue(ctx, name, Job{ID: "111#Sang"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if ok, _ := d.Claim(ctx, name, "111#Sang", time.Minute); !ok {
		t.Fatal("claim should succeed")
	}
	if err := d.Fail(ctx, name, "111#Sang", errors.New("gave up"), true); err != nil {
		t.Fatalf("fail: %v", err)
	}
	pending, err := d.Pending(ctx, name)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatal("failed (dead-letter) job must not show up as pending")
	}
	if ok, _ := d.Claim(ctx, name, "111#Sang", time.Minute); ok {
		t.Fatal("failed job must not be claimable")
	}
}
