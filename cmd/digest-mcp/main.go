package main

import (
	"context"
	"log"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sl6117/automations/internal/storage"
)

const artifactPrefix = "logs/runs/"

type listRunsInput struct {
	Since string `json:"since,omitempty" jsonschema:"earliest run date to include, YYYY-MM-DD; omit for all runs"`
}

type listRunsOutput struct {
	Keys []string `json:"keys" jsonschema:"artifact keys, one per run+language, oldest first"`
}

func main() {
	ctx := context.Background()

	store, err := storage.FromEnv(ctx)
	if err != nil {
		log.Fatalf("digest-mcp: storage: %v", err)
	}

	listRuns := func(ctx context.Context, req *mcp.CallToolRequest, in listRunsInput) (*mcp.CallToolResult, listRunsOutput, error) {
		keys, err := store.List(ctx, artifactPrefix)
		if err != nil {
			return nil, listRunsOutput{}, err
		}
		out := listRunsOutput{Keys: []string{}}
		for _, k := range keys {
			if in.Since == "" || strings.TrimPrefix(k, artifactPrefix) >= in.Since {
				out.Keys = append(out.Keys, k)
			}
		}
		return nil, out, nil
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "digest-mcp", Version: "v0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_runs",
		Description: "list digest run artifact keys, oldest first, optionally filtered by date",
	}, listRuns)

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
