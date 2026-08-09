package weeklydeepdive

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sl6117/automations/internal/ai"
)

func TestResearchFanOutPreservesOrder(t *testing.T) {
	var bothStarted sync.WaitGroup
	bothStarted.Add(2)
	releaseSlow := make(chan struct{})

	run := func(ctx context.Context, i int, q string) (researchResult, error) {
		bothStarted.Done()
		bothStarted.Wait()
		if i == 0 {
			<-releaseSlow // finishes last, but must still land in slot 0
		} else {
			close(releaseSlow)
		}
		return researchResult{
			Report: ResearchReport{Question: q},
			Usage:  ai.Usage{InputTokens: i + 1, OutputTokens: 1},
		}, nil
	}

	got, usage, err := researchFanOut(context.Background(), []string{"first", "second"}, run)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Question != "first" || got[1].Question != "second" {
		t.Fatalf("order broken: %+v", got)
	}
	if usage.InputTokens != 3 || usage.OutputTokens != 2 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestResearchFanOutFailFastCancelsSiblings(t *testing.T) {
	q1Running := make(chan struct{})

	run := func(ctx context.Context, i int, q string) (researchResult, error) {
		if i == 0 {
			<-q1Running
			return researchResult{}, errors.New("boom on q0")
		}
		close(q1Running)
		select {
		case <-ctx.Done():
			return researchResult{}, ctx.Err()
		case <-time.After(2 * time.Second):
			t.Fatal("q1 was not cancelled")
			return researchResult{}, nil
		}
	}

	_, _, err := researchFanOut(context.Background(), []string{"a", "b"}, run)
	if err == nil || err.Error() != "boom on q0" {
		t.Fatalf("err = %v, want boom on q0", err)
	}
}
