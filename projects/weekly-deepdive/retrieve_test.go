package weeklydeepdive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTokenizeDropsNoise(t *testing.T) {
	got := tokenize("The Fed raised rates — AI stocks fell!")
	joined := strings.Join(got, " ")
	for _, want := range []string{"fed", "raised", "rates", "ai", "stocks", "fell"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tokenize missing %q in %v", want, got)
		}
	}
	for _, stop := range []string{"the"} {
		for _, tok := range got {
			if tok == stop {
				t.Fatalf("tokenize kept stopword %q in %v", stop, got)
			}
		}
	}
}

func TestScoreOverlapRanksRelevantChunk(t *testing.T) {
	q := tokenize("OpenAI releases new model")
	hi := scoreOverlap(q, "- OpenAI shipped a new frontier model today")
	lo := scoreOverlap(q, "- Housing inventory ticked up in Austin")
	if hi <= lo {
		t.Fatalf("relevant chunk score %d should beat unrelated %d", hi, lo)
	}
}

func TestChunkDigestSplitsSections(t *testing.T) {
	digest := "## AI\n- model news\n- chip news\n\n## Econ\n- rates held\n"
	chunks := chunkDigest(digest)
	if len(chunks) < 2 {
		t.Fatalf("chunks = %v, want at least AI and Econ sections", chunks)
	}
}

func TestRetrieveDigestContextTopK(t *testing.T) {
	tools := fakeTools{call: func(name string, args json.RawMessage) (string, bool, error) {
		switch name {
		case "list_runs":
			return `{"keys":["logs/runs/a-twitter-digest-english.json","logs/runs/b-twitter-digest-korean.json"]}`, false, nil
		case "get_artifact":
			var in struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal(args, &in)
			if strings.Contains(in.Key, "korean") {
				return `{"artifact":{"language":"Korean","digest":"## AI\n- 한국어 뉴스 about OpenAI"}}`, false, nil
			}
			return `{"artifact":{"language":"English","digest":"## AI\n- OpenAI released a new model\n\n## Econ\n- mortgage rates flat\n"}}`, false, nil
		}
		t.Fatalf("unexpected tool %s", name)
		return "", false, nil
	}}
	got, err := retrieveDigestContext(context.Background(), tools, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		"OpenAI model release", 2, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Prior digest context") {
		t.Fatalf("missing header: %q", got)
	}
	if !strings.Contains(got, "OpenAI") {
		t.Fatalf("should retrieve OpenAI chunk: %q", got)
	}
	if strings.Contains(got, "한국어") {
		t.Fatalf("must skip non-English digests: %q", got)
	}
}

func TestRetrieveDigestContextEmptyQueryOrNoHits(t *testing.T) {
	tools := fakeTools{call: func(name string, args json.RawMessage) (string, bool, error) {
		if name == "list_runs" {
			return `{"keys":[]}`, false, nil
		}
		return "", false, nil
	}}
	got, err := retrieveDigestContext(context.Background(), tools, time.Now(), "anything", 3, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("no hits should render empty, got %q", got)
	}
}

func TestRenderArchiveContextCaps(t *testing.T) {
	chunks := []scoredChunk{
		{Text: strings.Repeat("word ", 50), Score: 5},
		{Text: "second chunk about rates", Score: 3},
	}
	got := renderArchiveContext(chunks, 60)
	if !strings.Contains(got, "Prior digest context") {
		t.Fatalf("missing header: %q", got)
	}
	// body after header should respect maxChars
	const header = "Prior digest context (our archive; not corroborated):\n"
	body := strings.TrimPrefix(got, header)
	if len(body) > 60 {
		t.Fatalf("body len %d exceeds maxChars 60:\n%q", len(body), body)
	}
}
