package scm

import "github.com/donovan-yohan/belayer/internal/model"

type ClimbDiff struct {
	ClimbID      string
	RepoName     string
	LinesChanged int
	Commits      []string
}

// CalculateStackSplits groups climbs into PR-sized chunks under the threshold.
func CalculateStackSplits(climbs []ClimbDiff, threshold int) []model.PRSplit {
	total := 0
	for _, c := range climbs {
		total += c.LinesChanged
	}
	if total <= threshold {
		split := model.PRSplit{StackPosition: 1}
		for _, c := range climbs {
			split.Commits = append(split.Commits, c.Commits...)
		}
		return []model.PRSplit{split}
	}
	// Greedy bin-packing
	var splits []model.PRSplit
	current := model.PRSplit{StackPosition: 1}
	currentSize := 0
	for _, c := range climbs {
		if currentSize+c.LinesChanged > threshold && currentSize > 0 {
			splits = append(splits, current)
			current = model.PRSplit{StackPosition: len(splits) + 1}
			currentSize = 0
		}
		current.Commits = append(current.Commits, c.Commits...)
		currentSize += c.LinesChanged
	}
	if len(current.Commits) > 0 {
		splits = append(splits, current)
	}
	return splits
}
