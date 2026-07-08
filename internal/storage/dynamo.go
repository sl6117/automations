package storage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// blobSK is the sort key for whole-value items (put/get)
// Append items use an RFC3339 nano timestamp sort key, so one
// partition holds either a single blob or an ordered stream of lines

const blobSK = "_"

// Dynamo stores blobs and append-streams in one DynamoDB table
// (pk = storage key, sk = blobSK or timestamp).
type Dynamo struct {
	client *dynamodb.Client
	table  string
}

// NewDynamo reads AWS credentials/region from the environment
// (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION) and targets
// the table named by DYNAMO_TABLE (default "automations")
func NewDynamo(ctx context.Context) (*Dynamo, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	table := os.Getenv("DYNAMO_TABLE")

	if table == "" {
		table = "automations"
	}

	return &Dynamo{client: dynamodb.NewFromConfig(cfg), table: table}, nil
}

// projectOf derives the project attribute from the key prefix,
// so a future GSI can query per project without a backfill migration
func projectOf(key string) string {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) >= 2 && parts[0] == "projects" {
		return parts[1]
	}
	return "framework"
}

func (d *Dynamo) put(ctx context.Context, key, sk string, data []byte) error {
	_, err := d.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(d.table),
		Item: map[string]types.AttributeValue{
			"pk":      &types.AttributeValueMemberS{Value: key},
			"sk":      &types.AttributeValueMemberS{Value: sk},
			"data":    &types.AttributeValueMemberB{Value: data},
			"project": &types.AttributeValueMemberS{Value: projectOf(key)},
		},
	})
	return err
}

func (d *Dynamo) Put(ctx context.Context, key string, data []byte) error {
	if err := d.put(ctx, key, blobSK, data); err != nil {
		return fmt.Errorf("put %q: %w", key, err)
	}
	return nil
}

func (d *Dynamo) Append(ctx context.Context, key string, data []byte) error {
	sk := time.Now().UTC().Format(time.RFC3339Nano)
	if err := d.put(ctx, key, sk, data); err != nil {
		return fmt.Errorf("append %q: %w", key, err)
	}
	return nil
}

// Get returns a blob item's value as-is, or an append-stream's lines
// joined with newlines (sk order = chronological order)
func (d *Dynamo) Get(ctx context.Context, key string) ([]byte, error) {
	var items []map[string]types.AttributeValue
	var startKey map[string]types.AttributeValue

	for {
		out, err := d.client.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(d.table),
			KeyConditionExpression: aws.String("pk = :pk"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": &types.AttributeValueMemberS{Value: key},
			},
			ConsistentRead:    aws.Bool(true),
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, fmt.Errorf("get %q: %w", key, err)
		}
		items = append(items, out.Items...)
		if out.LastEvaluatedKey == nil {
			break
		}
		startKey = out.LastEvaluatedKey
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("get %q: %w", key, ErrNotExist)
	}

	var buf bytes.Buffer
	for _, item := range items {
		data, ok := item["data"].(*types.AttributeValueMemberB)
		if !ok {
			return nil, fmt.Errorf("get %q: item missing data attribute", key)
		}
		if sk, ok := item["sk"].(*types.AttributeValueMemberS); ok && sk.Value == blobSK {
			return data.Value, nil
		}
		buf.Write(data.Value)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}
