package scoring

import "pricing-catalog/internal/evaluation/runner"

type Report struct {
	Total  int
	Passed int
	Failed int
	Score  float64
}

func Score(results []runner.Result) Report {
	report := Report{
		Total: len(results),
	}

	for _, result := range results {
		if result.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}

	if report.Total > 0 {
		report.Score = float64(report.Passed) / float64(report.Total) * 100
	}

	return report
}
