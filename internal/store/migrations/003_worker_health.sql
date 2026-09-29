CREATE TABLE worker_heartbeats (
    id text PRIMARY KEY,
    slots integer NOT NULL,
    last_seen timestamptz NOT NULL DEFAULT now()
);
