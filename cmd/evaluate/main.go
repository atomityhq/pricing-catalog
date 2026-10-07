package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pricing-catalog/internal/evaluation/cases"
	"pricing-catalog/internal/evaluation/runner"
	"pricing-catalog/internal/evaluation/scoring"
	"pricing-catalog/internal/pipeline"
)

func main() {
	var (
		task      string
		evalDir   string
		version   string
		generated string
	)

	flag.StringVar(&task, "task", "", "evaluation task ID")
	flag.StringVar(
		&evalDir,
		"private-eval-dir",
		"",
		"directory containing private evaluation data",
	)
	flag.StringVar(&version, "version", "evaluation", "snapshot version")
	flag.StringVar(
		&generated,
		"generated-at",
		"",
		"snapshot generation time in RFC3339 format",
	)
	flag.Parse()

	if task == "" || evalDir == "" {
		flag.Usage()
		os.Exit(2)
	}

	suitePath := filepath.Join(evalDir, task, "cases.yaml")
	suiteFile, err := os.Open(suitePath)
	if err != nil {
		fatalf("open evaluation cases: %v", err)
	}
	defer suiteFile.Close()

	suite, err := cases.Load(suiteFile)
	if err != nil {
		fatalf("load evaluation cases: %v", err)
	}

	if suite.Task != task {
		fatalf(
			"evaluation task mismatch: cases declare %q, requested %q",
			suite.Task,
			task,
		)
	}

	generatedAt := time.Now().UTC()
	if generated != "" {
		generatedAt, err = time.Parse(time.RFC3339Nano, generated)
		if err != nil {
			fatalf("parse generated-at: %v", err)
		}
	}

	results := make([]runner.Result, 0, len(suite.Cases))
	for i := range suite.Cases {
		result, err := runner.RunCase(
			context.Background(),
			suite,
			i,
			filepath.Join(evalDir, task, "fixtures"),
			pipeline.BuildOptions{
				Version:     version,
				GeneratedAt: generatedAt,
			},
		)
		if err != nil {
			fatalf("run case %q: %v", suite.Cases[i].ID, err)
		}
		results = append(results, result)
	}

	report := scoring.Score(results)

	for _, result := range results {
		status := "PASS"
		if !result.Passed {
			status = "FAIL"
		}

		if result.Error != nil {
			fmt.Printf("%s  %s: %v\n", status, result.CaseID, result.Error)
		} else {
			fmt.Printf("%s  %s\n", status, result.CaseID)
		}
	}

	fmt.Printf(
		"\nAutomated cases: %d/%d passed (%.1f%%)\n",
		report.Passed,
		report.Total,
		report.Score,
	)

	if report.Failed > 0 {
		os.Exit(1)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
