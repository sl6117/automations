package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sl6117/automations/internal/storage"
	twitterdigest "github.com/sl6117/automations/projects/twitter-digest"
)

const artifactPrefix = "logs/runs/"

// digestServer holds the tool handlers; store is injected so tests
// can use a filesystem root instead of the env-selected backend.
type digestServer struct {
	store storage.Store
}

type listRunsInput struct {
	Since string `json:"since,omitempty" jsonschema:"earliest run date to include, YYYY-MM-DD; omit for all runs"`
}

type listRunsOutput struct {
	Keys []string `json:"keys" jsonschema:"artifact keys, one per run+language, oldest first"`
}

func (s *digestServer) listRuns(ctx context.Context, req *mcp.CallToolRequest, in listRunsInput) (*mcp.CallToolResult, listRunsOutput, error) {
	keys, err := s.store.List(ctx, artifactPrefix)

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

type getArtifactInput struct {
	Key           string `json:"key" jsonschema:"artifact key as returned by list_runs"`
	IncludeTweets bool   `json:"includeTweets,omitempty" jsonschema:"include the kept source tweets (large); default false returns the digest and verdicts only"`
}

type getArtifactOutput struct {
	Artifact twitterdigest.Artifact `json:"artifact" jsonschema:"one run's record: digest text, eval results, judge verdicts; kept tweets omitted unless requested"`
}

func (s *digestServer) getArtifact(ctx context.Context, req *mcp.CallToolRequest, in getArtifactInput) (*mcp.CallToolResult, getArtifactOutput, error) {
	data, err := s.store.Get(ctx, in.Key)
	if err != nil {
		return nil, getArtifactOutput{}, fmt.Errorf("get %q: %w", in.Key, err)
	}

	var artifact twitterdigest.Artifact

	if err := json.Unmarshal(data, &artifact); err != nil {
		return nil, getArtifactOutput{}, fmt.Errorf("parse %q: %w", in.Key, err)
	}
	if !in.IncludeTweets {
		artifact.Kept = nil
	}
	return nil, getArtifactOutput{Artifact: artifact}, nil
}

func main() {
	ctx := context.Background()

	store, err := storage.FromEnv(ctx)
	if err != nil {
		log.Fatalf("digest-mcp: storage: %v", err)
	}

	s := &digestServer{store: store}

	server := mcp.NewServer(&mcp.Implementation{Name: "digest-mcp", Version: "v0.2.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_runs",
		Description: "list digest run artifact keys, oldest first, optionally filtered by date",
	}, s.listRuns)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_artifact",
		Description: "fetch one digest run artifact by key: digest text, eval results, judge verdicts; set includeTweets for the source tweets",
	}, s.getArtifact)

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
