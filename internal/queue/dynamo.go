package queue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Dynamo stores jobs as items in the shared automations table:
// pk queue#name, sk = jobId. Claim/Complete/Fail are UpdateItems
// whose conditionExpressions enforce the same guards Memory enforces
// under its mutex - DDB evaluates the condition and the write atomically, so racing workers cannot both win
type Dynamo struct {
	client *dynamodb.Client
	table  string
}

// NewDynamo reads AWS credentials/region from the environment and targets
// the table named by DYNAMO_TABLE (default "automations")
func NewDynamo(ctx context.Context) (*Dynamo, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	table := os.Getenv("DYNAMO_TABLE")
	if table == "" {
		table = "automations"
	}
	return &Dynamo{client: dynamodb.NewFromConfig(cfg), table: table}, nil
}

func pkOf(name string) string { return "queue#" + name }

func keyOf(name, id string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: pkOf(name)},
		"sk": &types.AttributeValueMemberS{Value: id},
	}
}

// leases are epoch milliseconds (attribute type N) s.t. claim conditionExpression can compare them
// numerically server-side
func millis(t time.Time) string {
	return strconv.FormatInt(t.UnixMilli(), 10)
}

// isConditionFailed reports whehter an error is DDB telling us
// conditionExpression rejected the write -> lost race condition
func isConditionFailed(err error) bool {
	var ccf *types.ConditionalCheckFailedException
	return errors.As(err, &ccf)
}

func (d *Dynamo) Enqueue(ctx context.Context, name string, job Job) error {

	item := map[string]types.AttributeValue{
		"pk":         &types.AttributeValueMemberS{Value: pkOf(name)},
		"sk":         &types.AttributeValueMemberS{Value: job.ID},
		"status":     &types.AttributeValueMemberS{Value: string(StatusPending)},
		"attempts":   &types.AttributeValueMemberN{Value: "0"},
		"leaseUntil": &types.AttributeValueMemberN{Value: "0"},
		"lastError":  &types.AttributeValueMemberS{Value: ""},
		"createdAt":  &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339Nano)},
		"project":    &types.AttributeValueMemberS{Value: name},
	}
	if len(job.Payload) > 0 {
		item["payload"] = &types.AttributeValueMemberB{Value: job.Payload}
	}

	_, err := d.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(d.table),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		if isConditionFailed(err) {
			return nil
		}
		return fmt.Errorf("enqueue %s#%s: %w", name, job.ID, err)
	}
	return nil
}

func (d *Dynamo) Pending(ctx context.Context, name string) ([]Job, error) {
	var jobs []Job
	var startKey map[string]types.AttributeValue

	for {
		out, err := d.client.Query(ctx, &dynamodb.QueryInput{
			TableName:                aws.String(d.table),
			KeyConditionExpression:   aws.String("pk = :pk"),
			FilterExpression:         aws.String("#s = :pending"),
			ExpressionAttributeNames: map[string]string{"#s": "status"},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk":      &types.AttributeValueMemberS{Value: pkOf(name)},
				":pending": &types.AttributeValueMemberS{Value: string(StatusPending)},
			},
			ConsistentRead:    aws.Bool(true),
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, fmt.Errorf("pending %s: %w", name, err)
		}
		for _, item := range out.Items {
			job, err := jobFromDDB(item)
			if err != nil {
				return nil, fmt.Errorf("pending %s: %w", name, err)
			}
			jobs = append(jobs, job)

		}
		if out.LastEvaluatedKey == nil {
			break
		}
		startKey = out.LastEvaluatedKey

	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	return jobs, nil
}

func (d *Dynamo) Claim(ctx context.Context, name, id string, lease time.Duration) (bool, error) {
	now := time.Now()

	_, err := d.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                aws.String(d.table),
		Key:                      keyOf(name, id),
		ConditionExpression:      aws.String("#s = :pending AND leaseUntil < :now"),
		UpdateExpression:         aws.String("SET leaseUntil = :until"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pending": &types.AttributeValueMemberS{Value: string(StatusPending)},
			":now":     &types.AttributeValueMemberN{Value: millis(now)},
			":until":   &types.AttributeValueMemberN{Value: millis(now.Add(lease))},
		},
	})
	if err != nil {
		if isConditionFailed(err) {
			return false, nil
		}
		return false, fmt.Errorf("claim %s#%s: %w", name, id, err)
	}
	return true, nil
}

func (d *Dynamo) Complete(ctx context.Context, name, id string) error {
	_, err := d.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(d.table),
		Key:       keyOf(name, id),
		// without this condition, completing a nonexistent job would
		// CREATE a phantom item with only pk/sk/status
		ConditionExpression:      aws.String("attribute_exists(pk)"),
		UpdateExpression:         aws.String("SET #s = :delivered"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":delivered": &types.AttributeValueMemberS{Value: string(StatusDelivered)},
		},
	})
	if err != nil {
		if isConditionFailed(err) {
			return fmt.Errorf("complete %s#%s: job not found", name, id)
		}
		return fmt.Errorf("complete %s#%s: %w", name, id, err)
	}
	return nil
}

func (d *Dynamo) Fail(ctx context.Context, name, id string, jobErr error, final bool) error {
	update := "SET attempts = if_not_exists(attempts, :zero) + :one, lastError = :err, leaseUntil = :zero"
	values := map[string]types.AttributeValue{
		":zero": &types.AttributeValueMemberN{Value: "0"},
		":one":  &types.AttributeValueMemberN{Value: "1"},
		":err":  &types.AttributeValueMemberS{Value: jobErr.Error()},
	}
	input := &dynamodb.UpdateItemInput{
		TableName:           aws.String(d.table),
		Key:                 keyOf(name, id),
		ConditionExpression: aws.String("attribute_exists(pk)"),
	}
	if final {
		update += ", #s = :failed"
		input.ExpressionAttributeNames = map[string]string{"#s": "status"}
		values[":failed"] = &types.AttributeValueMemberS{Value: string(StatusFailed)}
	}
	input.UpdateExpression = aws.String(update)
	input.ExpressionAttributeValues = values
	if _, err := d.client.UpdateItem(ctx, input); err != nil {
		if isConditionFailed(err) {
			return fmt.Errorf("fail %s#%s: job not found", name, id)
		}
		return fmt.Errorf("fail %s#%s: %w", name, id, err)
	}
	return nil
}

// jobFrom converts a DynamoDB item back into a Job
func jobFromDDB(item map[string]types.AttributeValue) (Job, error) {
	var job Job
	sk, ok := item["sk"].(*types.AttributeValueMemberS)
	if !ok {
		return job, errors.New("item missing sk")
	}
	job.ID = sk.Value
	if v, ok := item["payload"].(*types.AttributeValueMemberB); ok {
		job.Payload = v.Value
	}
	if v, ok := item["status"].(*types.AttributeValueMemberS); ok {
		job.Status = Status(v.Value)
	}
	if v, ok := item["attempts"].(*types.AttributeValueMemberN); ok {
		n, err := strconv.Atoi(v.Value)
		if err != nil {
			return job, fmt.Errorf("attempts: %w", err)
		}
		job.Attempts = n
	}
	if v, ok := item["leaseUntil"].(*types.AttributeValueMemberN); ok {
		ms, err := strconv.ParseInt(v.Value, 10, 64)
		if err != nil {
			return job, fmt.Errorf("leaseUntil: %w", err)
		}
		if ms > 0 {
			job.LeaseUntil = time.UnixMilli(ms).UTC()
		}
	}
	if v, ok := item["lastError"].(*types.AttributeValueMemberS); ok {
		job.LastError = v.Value
	}
	if v, ok := item["createdAt"].(*types.AttributeValueMemberS); ok {
		t, err := time.Parse(time.RFC3339Nano, v.Value)
		if err != nil {
			return job, fmt.Errorf("createdAt: %w", err)
		}
		job.CreatedAt = t
	}
	return job, nil
}
