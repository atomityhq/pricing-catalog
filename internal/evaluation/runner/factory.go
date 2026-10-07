package runner

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"pricing-catalog/internal/evaluation/cases"
	"pricing-catalog/internal/pipeline"
	"pricing-catalog/pkg/connector"
)

type Factory func(io.Reader) connector.Connector

// RunSuite runs every private evaluation case using a maintainer-supplied
// connector factory.
//
// The factory is intentionally supplied by the caller rather than registered
// in the public repository. This keeps task-specific evaluation wiring out of
// the candidate-visible source tree.
func RunSuite(
	ctx context.Context,
	suite *cases.Suite,
	fixtureDir string,
	factory Factory,
	options pipeline.BuildOptions,
) ([]Result, error) {
	if suite == nil {
		return nil, fmt.Errorf("evaluation suite is nil")
	}
	if factory == nil {
		return nil, fmt.Errorf("evaluation connector factory is nil")
	}
	if fixtureDir == "" {
		return nil, fmt.Errorf("evaluation fixture directory is required")
	}

	results := make([]Result, 0, len(suite.Cases))

	for _, testCase := range suite.Cases {
		if testCase == nil {
			return nil, fmt.Errorf("evaluation case is nil")
		}
		if testCase.Input == nil || testCase.Input.Fixture == "" {
			return nil, fmt.Errorf(
				"case %q: input.fixture is required",
				testCase.ID,
			)
		}

		fixturePath := filepath.Join(fixtureDir, testCase.Input.Fixture)

		caseSuite := &cases.Suite{
			Version: suite.Version,
			Task:    suite.Task,
			Cases:   []*cases.Case{testCase},
		}

		caseResults, runErr := Run(
			ctx,
			factory,
			caseSuite,
			fixturePath,
			options,
		)

		if runErr != nil {
			return nil, runErr
		}

		if len(caseResults) != 1 {
			return nil, fmt.Errorf(
				"case %q: expected one result, got %d",
				testCase.ID,
				len(caseResults),
			)
		}

		results = append(results, caseResults[0])
	}

	return results, nil
}
