-- anonymous boards (nobody sees who wrote what) and planning poker
ALTER TABLE boards ADD COLUMN anonymous INTEGER NOT NULL DEFAULT 0;
ALTER TABLE boards ADD COLUMN poker TEXT NOT NULL DEFAULT '{}';
