package measure

import (
	"encoding/json"
	"math"
)

// AxeCounts counts violated rules by impact. A rule broken on twenty
// elements is one problem to fix, so nodes are not counted.
func AxeCounts(results []byte) (map[string]int, error) {
	var parsed struct {
		Violations []struct {
			Impact string `json:"impact"`
		} `json:"violations"`
	}
	if err := json.Unmarshal(results, &parsed); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, violation := range parsed.Violations {
		counts[violation.Impact]++
	}
	return counts, nil
}

// LighthouseScores reads each category's score as 0 to 100. A null
// score means Lighthouse could not judge it, which is left out rather
// than reported as zero.
func LighthouseScores(report []byte) (map[string]int, error) {
	var parsed struct {
		Categories map[string]struct {
			Score *float64 `json:"score"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(report, &parsed); err != nil {
		return nil, err
	}
	scores := map[string]int{}
	for name, category := range parsed.Categories {
		if category.Score != nil {
			scores[name] = int(math.Round(*category.Score * 100))
		}
	}
	return scores, nil
}
