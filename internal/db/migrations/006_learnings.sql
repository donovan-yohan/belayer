-- 006_learnings.sql: Add learnings table for review loop knowledge capture

CREATE TABLE IF NOT EXISTS learnings (
    id TEXT PRIMARY KEY,
    crag_id TEXT NOT NULL,
    problem_id TEXT,
    category TEXT NOT NULL,
    description TEXT NOT NULL,
    recommendation TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'medium',
    resolved INTEGER NOT NULL DEFAULT 0,
    access_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_learnings_crag_active ON learnings(crag_id, resolved);
CREATE INDEX idx_learnings_category ON learnings(category);
