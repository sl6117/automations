package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sl6117/automations/internal/obs"
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

type getVerdictsInput struct {
	Since string `json:"since,omitempty" jsonschema:"earliest run date to include, YYYY-MM-DD; omit for all runs"`
}

type verdictSummary struct {
	Key        string   `json:"key"`
	Language   string   `json:"language"`
	Judged     bool     `json:"judged" jsonschema:"false when the run has no judge report"`
	Pass       bool     `json:"pass" jsonschema:"true when judged and every dimension passed"`
	Failures   []string `json:"failures,omitempty" jsonschema:"failing dimensions with the judge's reasons"`
	JudgeError string   `json:"judgeError,omitempty"`
}

type getVerdictsOutput struct {
	Verdicts []verdictSummary `json:"verdicts" jsonschema:"one row per run+language, oldest first"`
}

func (s *digestServer) getVerdicts(ctx context.Context, req *mcp.CallToolRequest, in getVerdictsInput) (*mcp.CallToolResult, getVerdictsOutput, error) {
	keys, err := s.store.List(ctx, artifactPrefix)
	if err != nil {
		return nil, getVerdictsOutput{}, err
	}
	out := getVerdictsOutput{Verdicts: []verdictSummary{}}

	for _, k := range keys {
		if in.Since != "" && strings.TrimPrefix(k, artifactPrefix) < in.Since {
			continue
		}
		data, err := s.store.Get(ctx, k)
		if err != nil {
			return nil, getVerdictsOutput{}, fmt.Errorf("get %q: %w", k, err)
		}
		var artifact twitterdigest.Artifact
		if err := json.Unmarshal(data, &artifact); err != nil {
			return nil, getVerdictsOutput{}, fmt.Errorf("parse: %q: %w", k, err)
		}
		row := verdictSummary{Key: k, Language: artifact.Language, JudgeError: artifact.JudgeError}
		if artifact.Judge != nil {
			row.Judged = true
			row.Failures = artifact.Judge.Failures()
			row.Pass = len(row.Failures) == 0
		}
		out.Verdicts = append(out.Verdicts, row)
	}
	return nil, out, nil
}

type getCostInput struct {
	Month string `json:"month,omitempty" jsonschema:"restrict to one month, YYYY-MM; omit for all months"`
}

type monthCost struct {
	Month       string  `json:"month"`
	Runs        int     `json:"runs"`
	Tokens      int     `json:"tokens"`
	SourceReads int     `json:"sourceReads" jsonschema:"billed X API reads; the monthly spend cap's unit"`
	CostUSD     float64 `json:"costUsd"`
}

type getCostOutput struct {
	Months []monthCost `json:"months" jsonschema:"per-month totals, oldest first"`
}

func (s *digestServer) getCost(ctx context.Context, req *mcp.CallToolRequest, in getCostInput) (*mcp.CallToolResult, getCostOutput, error) {
	data, err := s.store.Get(ctx, obs.CostLogKey)
	if err != nil {
		return nil, getCostOutput{}, fmt.Errorf("read cost log: %w", err)
	}
	totals := map[string]*monthCost{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var run obs.Run
		if err := json.Unmarshal([]byte(line), &run); err != nil {
			return nil, getCostOutput{}, fmt.Errorf("parse: %w", err)
		}
		month := run.Timestamp
		if len(month) >= 7 {
			month = month[:7]
		}
		if in.Month != "" && month != in.Month {
			continue
		}
		m := totals[month]

		if m == nil {
			m = &monthCost{Month: month}
			totals[month] = m
		}
		m.Runs++
		m.Tokens += run.InputTokens + run.OutputTokens
		m.SourceReads += run.SourceReads
		m.CostUSD += run.CostUSD
	}
	out := getCostOutput{Months: []monthCost{}}
	for _, m := range totals {
		out.Months = append(out.Months, *m)
	}
	sort.Slice(out.Months, func(i, j int) bool { return out.Months[i].Month < out.Months[j].Month })
	return nil, out, nil
}

func main() {
	ctx := context.Background()

	store, err := storage.FromEnv(ctx)
	if err != nil {
		log.Fatalf("digest-mcp: storage: %v", err)
	}

	s := &digestServer{store: store}

	server := mcp.NewServer(&mcp.Implementation{Name: "digest-mcp", Version: "v0.3.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_runs",
		Description: "list digest run artifact keys, oldest first, optionally filtered by date",
	}, s.listRuns)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_artifact",
		Description: "fetch one digest run artifact by key: digest text, eval results, judge verdicts; set includeTweets for the source tweets",
	}, s.getArtifact)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_verdicts",
		Description: "summarize judge verdicts across runs: pass/fail per run+language with failure reasons; optionally since a date",
	}, s.getVerdicts)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_cost",
		Description: "per-month LLM token cost and billed X API reads, optionally for one month",
	}, s.getCost)

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
