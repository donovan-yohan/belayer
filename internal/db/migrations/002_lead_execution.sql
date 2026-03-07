-- 002_lead_execution.sql: Lead goal tracking for execution loop

CREATE TABLE IF NOT EXISTS lead_goals (
    id TEXT PRIMARY KEY,
    lead_id TEXT NOT NULL REFERENCES leads(id),
    goal_index INTEGER NOT NULL,
    description TEXT DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    attempt INTEGER NOT NULL DEFAULT 0,
    output TEXT DEFAULT '',
    verdict_json TEXT DEFAULT '',
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_lead_goals_lead ON lead_goals(lead_id);
CREATE INDEX IF NOT EXISTS idx_lead_goals_status ON lead_goals(status);
