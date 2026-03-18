-- 005_environments.sql: Add environments table and worktree_path to climbs

CREATE TABLE IF NOT EXISTS environments (
    problem_id TEXT NOT NULL REFERENCES problems(id),
    provider_command TEXT NOT NULL,
    env_name TEXT NOT NULL,
    env_json TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (problem_id)
);

ALTER TABLE climbs ADD COLUMN worktree_path TEXT NOT NULL DEFAULT '';
