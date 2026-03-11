-- New tables for planning & review hats

CREATE TABLE tracker_issues (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT,
    comments_json TEXT,
    labels_json TEXT,
    priority TEXT,
    assignee TEXT,
    url TEXT,
    raw_json TEXT,
    problem_id TEXT,
    synced_at TIMESTAMP NOT NULL,
    FOREIGN KEY (problem_id) REFERENCES problems(id)
);

CREATE TABLE pull_requests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    problem_id TEXT NOT NULL,
    repo_name TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    url TEXT NOT NULL,
    stack_position INTEGER DEFAULT 1,
    stack_size INTEGER DEFAULT 1,
    ci_status TEXT DEFAULT 'pending',
    ci_fix_count INTEGER DEFAULT 0,
    review_status TEXT DEFAULT 'pending',
    state TEXT DEFAULT 'open',
    last_polled_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (problem_id) REFERENCES problems(id)
);

CREATE TABLE pr_reactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pr_id INTEGER NOT NULL,
    trigger_type TEXT NOT NULL,
    trigger_payload TEXT,
    action_taken TEXT NOT NULL,
    lead_id TEXT,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (pr_id) REFERENCES pull_requests(id)
);

ALTER TABLE problems ADD COLUMN tracker_issue_id TEXT REFERENCES tracker_issues(id);

CREATE INDEX idx_tracker_issues_problem ON tracker_issues(problem_id);
CREATE INDEX idx_pull_requests_problem ON pull_requests(problem_id);
CREATE INDEX idx_pull_requests_state ON pull_requests(state);
CREATE INDEX idx_pr_reactions_pr ON pr_reactions(pr_id);
