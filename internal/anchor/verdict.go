package anchor

// VerdictJSON is the structured output an anchor writes to VERDICT.json.
type VerdictJSON struct {
	Verdict string                `json:"verdict"` // "approve" or "reject"
	Repos   map[string]RepoVerdict `json:"repos"`
}

// RepoVerdict contains the anchor's assessment of a single repo.
type RepoVerdict struct {
	Status string   `json:"status"`  // "pass" or "fail"
	Climbs []string `json:"climbs"`  // correction climb descriptions (when status is "fail")
}
