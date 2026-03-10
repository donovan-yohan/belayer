-- 002_rename_crag.sql: Rename tables/columns to climbing-themed terminology
-- tasks -> problems, goals -> climbs
-- task_id -> problem_id, goal_id -> climb_id, goals_json -> climbs_json

-- Rename tables
ALTER TABLE tasks RENAME TO problems;
ALTER TABLE goals RENAME TO climbs;

-- Rename columns in problems (was tasks)
ALTER TABLE problems RENAME COLUMN goals_json TO climbs_json;

-- Rename columns in climbs (was goals)
ALTER TABLE climbs RENAME COLUMN task_id TO problem_id;

-- Rename columns in events
ALTER TABLE events RENAME COLUMN task_id TO problem_id;
ALTER TABLE events RENAME COLUMN goal_id TO climb_id;

-- Rename columns in spotter_reviews
ALTER TABLE spotter_reviews RENAME COLUMN task_id TO problem_id;

-- Drop old indexes (they reference the old table names)
DROP INDEX IF EXISTS idx_tasks_instance;
DROP INDEX IF EXISTS idx_tasks_status;
DROP INDEX IF EXISTS idx_goals_task;
DROP INDEX IF EXISTS idx_goals_status;
DROP INDEX IF EXISTS idx_events_task;
DROP INDEX IF EXISTS idx_spotter_reviews_task;

-- Recreate indexes with new names
CREATE INDEX IF NOT EXISTS idx_problems_instance ON problems(instance_id);
CREATE INDEX IF NOT EXISTS idx_problems_status ON problems(status);
CREATE INDEX IF NOT EXISTS idx_climbs_problem ON climbs(problem_id);
CREATE INDEX IF NOT EXISTS idx_climbs_status ON climbs(status);
CREATE INDEX IF NOT EXISTS idx_events_problem ON events(problem_id);
CREATE INDEX IF NOT EXISTS idx_spotter_reviews_problem ON spotter_reviews(problem_id);
