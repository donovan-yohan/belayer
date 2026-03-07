-- 003_task_intake.sql: Add sufficiency tracking to tasks

ALTER TABLE tasks ADD COLUMN sufficiency_checked INTEGER DEFAULT 0;
