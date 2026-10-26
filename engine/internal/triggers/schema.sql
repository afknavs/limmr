-- Add to hub.db in a versioned migration. Enable PRAGMA foreign_keys = ON on
-- every connection. Timestamps are UTC RFC3339Nano; definition_json is Record.
CREATE TABLE IF NOT EXISTS trigger_watches (
    id TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active','paused','stopped','expired')),
    expires_at TEXT,
    definition_json TEXT NOT NULL CHECK (json_valid(definition_json)),
    CHECK (json_extract(definition_json, '$.id') IS id),
    CHECK (json_extract(definition_json, '$.owner') IS owner),
    CHECK (json_extract(definition_json, '$.status') IS status)
);
CREATE INDEX IF NOT EXISTS trigger_watches_status_expiry ON trigger_watches(status, expires_at);
-- Instructions may only be appended, and the original grant cannot be replaced.
-- Store.Update must preserve every older instruction, including its timestamp.
CREATE TRIGGER IF NOT EXISTS trigger_instructions_append_only
BEFORE UPDATE OF definition_json ON trigger_watches
WHEN json_array_length(COALESCE(json_extract(NEW.definition_json, '$.instructions'), '[]'))
       < json_array_length(COALESCE(json_extract(OLD.definition_json, '$.instructions'), '[]'))
  OR EXISTS (
    SELECT 1 FROM json_each(OLD.definition_json, '$.instructions') AS prior
    WHERE json_extract(NEW.definition_json, '$.instructions[' || prior.key || ']') IS NOT prior.value
  )
  OR json_extract(NEW.definition_json, '$.created_via') IS NOT json_extract(OLD.definition_json, '$.created_via')
BEGIN
    SELECT RAISE(ABORT, 'trigger instructions and provenance are append-only');
END;
CREATE TABLE IF NOT EXISTS trigger_hits (
    id TEXT PRIMARY KEY,
    watch_id TEXT NOT NULL REFERENCES trigger_watches(id) ON DELETE CASCADE,
    at TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('feed','notify','digest','park_suggestion','wake_session')),
    state TEXT NOT NULL CHECK (state IN ('recorded','completed','failed','cancelled')),
    messages_json TEXT NOT NULL CHECK (json_valid(messages_json))
);
CREATE INDEX IF NOT EXISTS trigger_hits_watch_at ON trigger_hits(watch_id, at, id);
CREATE TABLE IF NOT EXISTS trigger_runs (
    id TEXT PRIMARY KEY,
    handoff_id TEXT NOT NULL REFERENCES trigger_watches(id) ON DELETE CASCADE,
    hit_id TEXT NOT NULL REFERENCES trigger_hits(id) ON DELETE CASCADE,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    exit_code INTEGER NOT NULL,
    timed_out INTEGER NOT NULL DEFAULT 0 CHECK (timed_out IN (0,1)),
    log_path TEXT,
    state TEXT NOT NULL CHECK (state IN ('running','completed','failed','cancelled'))
);
CREATE INDEX IF NOT EXISTS trigger_runs_handoff_at ON trigger_runs(handoff_id, started_at, id);
CREATE UNIQUE INDEX IF NOT EXISTS trigger_one_running_session ON trigger_runs(handoff_id) WHERE state = 'running';
CREATE TABLE IF NOT EXISTS trigger_replies (
    id INTEGER PRIMARY KEY,
    handoff_id TEXT NOT NULL REFERENCES trigger_watches(id) ON DELETE CASCADE,
    at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS trigger_replies_handoff_at ON trigger_replies(handoff_id, at);
-- ReserveReply: BEGIN IMMEDIATE; count rows with at > now-1h; if below max,
-- insert the reservation; COMMIT. Count and insert must share one transaction.
-- A slot is consumed before attempting delivery and retained if delivery fails.
