//go:build integration

package storage

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

// cleanup removes every item under key so test data doesn't accumulate
// in the real table.
func cleanup(t *testing.T, d *Dynamo, key string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		out, err := d.client.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(d.table),
			KeyConditionExpression: aws.String("pk = :pk"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": &types.AttributeValueMemberS{Value: key},
			},
		})
		if err != nil {
			t.Logf("leanup query %q: %v", key, err)
			return
		}
		for _, item := range out.Items {
			_, err := d.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
				TableName: aws.String(d.table),
				Key:       map[string]types.AttributeValue{"pk": item["pk"], "sk": item["sk"]},
			})
			if err != nil {
				t.Logf("cleanup delete %q: %v", key, err)
			}
		}
	})
}

func TestDynamoGetMissingReturnsErrNotExist(t *testing.T) {
	d := newTestDynamo(t)
	_, err := d.Get(context.Background(), "test/never-written")
	if !errors.Is(err, ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}

func TestDynamoPutThenGetRoundTrips(t *testing.T) {
	d := newTestDynamo(t)
	ctx := context.Background()
	key := fmt.Sprintf("test/blob-%d.json", time.Now().UnixNano())
	cleanup(t, d, key)

	if err := d.Put(ctx, key, []byte(`{"x":1}`)); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := d.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != `{"x":1}` {
		t.Fatalf("round trip mismatch: %q", got)
	}
}

func TestDynamoAppendAccumulateLines(t *testing.T) {
	d := newTestDynamo(t)
	ctx := context.Background()

	key := fmt.Sprintf("test/stream-%d.jsonl", time.Now().UnixNano())
	cleanup(t, d, key)

	if err := d.Append(ctx, key, []byte(`{"n":1}`)); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := d.Append(ctx, key, []byte(`{"n":2}`)); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	got, err := d.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "{\"n\":1}\n{\"n\":2}\n" {
		t.Fatalf("unexpected content: %q", got)
	}
}
