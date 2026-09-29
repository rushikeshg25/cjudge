CREATE TABLE rate_limits (
    principal_id text PRIMARY KEY REFERENCES principals(id),
    window_start timestamptz NOT NULL,
    requests integer NOT NULL
);
CREATE INDEX submissions_owner_cursor ON submissions(owner_id,id DESC);
