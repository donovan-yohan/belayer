package envprovider

// ServiceStatus represents the status of a single service.
type ServiceStatus struct {
	Status string `json:"status"`
	Port   int    `json:"port"`
	Uptime string `json:"uptime"`
}

// SnapshotInfo describes a restored snapshot.
type SnapshotInfo struct {
	Name       string `json:"name"`
	RestoredAt string `json:"restored_at"`
	Stale      bool   `json:"stale"`
}

// WorktreeInfo describes a worktree attached to an environment.
type WorktreeInfo struct {
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Path    string `json:"path"`
	EnvFile string `json:"env_file,omitempty"`
}

// WorktreeStatusInfo describes the live status of a worktree.
type WorktreeStatusInfo struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
	Dirty  bool   `json:"dirty"`
}

// LogLine is a single log entry.
type LogLine struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

// EnvSummary is a brief summary of an environment in a list.
type EnvSummary struct {
	Name          string `json:"name"`
	Index         int    `json:"index"`
	WorktreeCount int    `json:"worktree_count"`
	CreatedAt     string `json:"created_at"`
}

// CreateEnvResponse is returned when a new environment is created.
type CreateEnvResponse struct {
	Status    string                    `json:"status"`
	Name      string                    `json:"name"`
	Index     int                       `json:"index"`
	Env       map[string]string         `json:"env,omitempty"`
	Services  map[string]ServiceStatus  `json:"services,omitempty"`
	Worktrees []WorktreeInfo            `json:"worktrees,omitempty"`
}

// AddWorktreeResponse is returned when a worktree is added to an environment.
type AddWorktreeResponse struct {
	Status  string `json:"status"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Path    string `json:"path"`
	EnvFile string `json:"env_file,omitempty"`
}

// ResetEnvResponse is returned after an environment reset.
type ResetEnvResponse struct {
	Status     string `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	Snapshot   string `json:"snapshot"`
}

// StatusEnvResponse is returned for environment status queries.
type StatusEnvResponse struct {
	Status    string                    `json:"status"`
	Name      string                    `json:"name"`
	Index     int                       `json:"index"`
	Services  map[string]ServiceStatus  `json:"services"`
	Snapshot  *SnapshotInfo             `json:"snapshot,omitempty"`
	Worktrees []WorktreeStatusInfo      `json:"worktrees"`
}

// LogsEnvResponse is returned for environment log queries.
type LogsEnvResponse struct {
	Status  string    `json:"status"`
	Service string    `json:"service"`
	Lines   []LogLine `json:"lines"`
}

// ListEnvsResponse is returned when listing all environments.
type ListEnvsResponse struct {
	Status       string       `json:"status"`
	Environments []EnvSummary `json:"environments"`
}

// ErrorResponse is returned on error.
type ErrorResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Code   string `json:"code"`
}
