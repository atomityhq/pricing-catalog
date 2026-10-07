package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"pricing-catalog/internal/evaluation/cases"
	"pricing-catalog/internal/evaluation/registry"
	"pricing-catalog/internal/pipeline"
)

// RunCase constructs the connector from the case's private fixture and runs
// the evaluation against it.
func RunCase(
	ctx context.Context,
	suite *cases.Suite,
	caseIndex int,
	fixtureDir string,
	options pipeline.BuildOptions,
) (Result, error) {
	if suite == nil {
		return Result{}, fmt.Errorf("evaluation suite is nil")
	}
	if caseIndex < 0 || caseIndex >= len(suite.Cases) {
		return Result{}, fmt.Errorf("evaluation case index %d out of range", caseIndex)
	}
	if fixtureDir == "" {
		return Result{}, fmt.Errorf("evaluation fixture directory is required")
	}

	testCase := suite.Cases[caseIndex]
	if testCase == nil {
		return Result{}, fmt.Errorf("evaluation case %d is nil", caseIndex)
	}
	if testCase.Input == nil || testCase.Input.Fixture == "" {
		return Result{}, fmt.Errorf("case %q: input.fixture is required", testCase.ID)
	}

	fixturePath := filepath.Join(fixtureDir, testCase.Input.Fixture)
	file, err := os.Open(fixturePath)
	if err != nil {
		return Result{}, fmt.Errorf(
			"open fixture %q: %w",
			fixturePath,
			err,
		)
	}
	defer file.Close()

	conn, err := registry.New(suite.Task, file)
	if err != nil {
		return Result{}, err
	}

	results, err := Run(ctx, conn, &cases.Suite{
		Version: suite.Version,
		Task:    suite.Task,
		Cases:   []*cases.Case{testCase},
	}, options)
	if err != nil {
		return Result{}, err
	}
	if len(results) != 1 {
		return Result{}, fmt.Errorf(
			"expected one evaluation result, got %d",
			len(results),
		)
	}

	return results[0], nil
}
