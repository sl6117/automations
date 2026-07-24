// auto-lambda is the Lambda entrypoint for the automation runner: EventBridge
// (or a manual invoke) sends {"project": "...", "dryRun": bool} and the handler
// runs that project exactly as `auto run <project>` would.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/sl6117/automations/internal/runner"

	// Project registrations, mirroring cmd/auto/main.go.
	_ "github.com/sl6117/automations/projects/hello"
	_ "github.com/sl6117/automations/projects/twitter-digest"
	_ "github.com/sl6117/automations/projects/weekly-deepdive"
)

type Event struct {
	Project string `json:"project"`
	DryRun  bool   `json:"dryRun"`
}

const paramPrefix = "/automations/"

// loadSecrets copies /automations/* SSM parameters into process env vars at
// cold start so existing os.Getenv call sites work unchanged. Env vars already
// set on the function win over parameters.
func loadSecrets(ctx context.Context) error {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("aws config: %w", err)
	}
	client := ssm.NewFromConfig(cfg)
	var next *string
	for {
		out, err := client.GetParametersByPath(ctx, &ssm.GetParametersByPathInput{
			Path:           aws.String(paramPrefix),
			WithDecryption: aws.Bool(true),
			NextToken:      next,
		})
		if err != nil {
			return fmt.Errorf("ssm get %s: %w", paramPrefix, err)
		}
		for _, p := range out.Parameters {
			name := strings.TrimPrefix(*p.Name, paramPrefix)
			if os.Getenv(name) == "" {
				os.Setenv(name, *p.Value)
			}
		}
		if out.NextToken == nil {
			return nil
		}
		next = out.NextToken
	}
}

func handle(ctx context.Context, ev Event) error {
	project, ok := runner.Get(ev.Project)
	if !ok {
		return fmt.Errorf("project %q not found", ev.Project)
	}
	rt := &runner.Runtime{
		DryRun:     ev.DryRun,
		Log:        log.New(os.Stdout, "", 0),
		ProjectDir: filepath.Join("projects", ev.Project),
	}
	return project.Run(ctx, rt)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := loadSecrets(ctx); err != nil {
		log.Fatalf("auto-lambda: load secrets: %v", err)
	}
	lambda.Start(handle)
}
