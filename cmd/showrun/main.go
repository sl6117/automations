// showrun prints stored run artifacts so a human can read them.
// Usage: showrun                -> list artifact keys
//
//	showrun [-tweets]<key>         -> prints one artifact (digest + judge verdicts)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"github.com/sl6117/automations/internal/storage"
	twitterdigest "github.com/sl6117/automations/projects/twitter-digest"
)

const (
	bold  = "\033[1m"
	dim   = "\033[2m"
	green = "\033[32m"
	red   = "\033[31m"
	reset = "\033[0m"
)

func main() {
	tweets := flag.Bool("tweets", false, "also print the kept source tweets")
	flag.Parse()

	ctx := context.Background()
	store, err := storage.FromEnv(ctx)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	if flag.NArg() == 0 {
		keys, err := store.List(ctx, "logs/runs/")
		if err != nil {
			log.Fatalf("list: %v", err)
		}
		for _, k := range keys {
			fmt.Println(k)
		}
		return
	}

	key := flag.Arg(0)
	data, err := store.Get(ctx, key)
	if err != nil {
		log.Fatalf("get %s: %v", key, err)
	}
	var a twitterdigest.Artifact
	if err := json.Unmarshal(data, &a); err != nil {
		log.Fatalf("unmarshal %s: %v", key, err)
	}
	fmt.Printf("%sts=%s model=%s language=%s tokens=%d/%d%s\n", dim, a.Timestamp, a.Model, a.Language, a.InputTokens, a.OutputTokens, reset)
	fmt.Println(bold + "\n========== DIGEST ==========" + reset)
	fmt.Println(a.Digest)
	fmt.Println(bold + "\n========== JUDGE ===========" + reset)
	switch {
	case a.JudgeError != "":
		fmt.Println(red + "judge error: " + reset + a.JudgeError)
	case a.Judge == nil:
		fmt.Println("not judged")
	default:
		for _, d := range []struct {
			name string
			v    twitterdigest.Verdict
		}{
			{"faithfulness", a.Judge.Faithfulness},
			{"topicRouting", a.Judge.TopicRouting},
			{"coverage", a.Judge.Coverage},
			{"clarity", a.Judge.Clarity},
		} {
			status := green + "PASS" + reset
			if !d.v.Pass {
				status = red + "FAIL" + reset
			}
			fmt.Printf("%s%-13s%s %s\n", bold, d.name, reset, status)
			if d.v.Reason != "" {
				fmt.Printf("    %s\n\n", d.v.Reason)
			}
		}
	}
	if *tweets {
		fmt.Println(bold + "\n======== KEPT TWEETS ========" + reset)
		for _, t := range a.Kept {
			fmt.Printf("\n%s@%s%s  %s\n", bold, t.Handle, reset, t.Text)
			fmt.Printf("%s%s%s\n", dim, t.URL, reset)
		}
	}
}
