-- 003_rename_instance_to_crag.sql: Complete the crag rename at the data layer
-- instances -> crags, instance_id -> crag_id

ALTER TABLE instances RENAME TO crags;

ALTER TABLE problems RENAME COLUMN instance_id TO crag_id;

-- Drop old index and recreate with new name
DROP INDEX IF EXISTS idx_problems_instance;
CREATE INDEX IF NOT EXISTS idx_problems_crag ON problems(crag_id);
