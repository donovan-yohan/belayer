package spotter

// VerdictJSON is the structured output a spotter writes to VERDICT.json.
type VerdictJSON struct {
	Verdict string                `json:"verdict"` // "approve" or "reject"
	Repos   map[string]RepoVerdict `json:"repos"`
}

// RepoVerdict contains the spotter's assessment of a single repo.
type RepoVerdict struct {
	Status string   `json:"status"` // "pass" or "fail"
	Goals  []string `json:"goals"`  // correction goal descriptions (when status is "fail")
}
