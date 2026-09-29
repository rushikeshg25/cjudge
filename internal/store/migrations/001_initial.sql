CREATE TABLE principals (
    id text PRIMARY KEY CHECK (length(id) = 32),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    admin boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE api_keys (
    hash bytea PRIMARY KEY,
    principal_id text NOT NULL REFERENCES principals(id),
    revoked boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE problems (
    id text PRIMARY KEY CHECK (length(id) = 32),
    title text NOT NULL,
    statement text NOT NULL,
    checker text NOT NULL CHECK (checker IN ('tokens', 'exact')),
    limits jsonb NOT NULL,
    tests jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE submissions (
    id text PRIMARY KEY CHECK (length(id) = 32),
    owner_id text NOT NULL REFERENCES principals(id),
    problem_id text NOT NULL REFERENCES problems(id),
    language text NOT NULL CHECK (language IN ('cpp20','python3','go')),
    source text NOT NULL,
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL,
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','running','finished')),
    attempts integer NOT NULL DEFAULT 0,
    lease_token text,
    lease_until timestamptz,
    available_at timestamptz NOT NULL DEFAULT now(),
    result jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(owner_id, idempotency_key),
    CHECK ((state = 'running') = (lease_token IS NOT NULL AND lease_until IS NOT NULL)),
    CHECK ((state = 'finished') = (result IS NOT NULL))
);
CREATE INDEX submissions_ready ON submissions (available_at, created_at) WHERE state = 'queued';
CREATE INDEX submissions_expired ON submissions (lease_until) WHERE state = 'running';
CREATE INDEX submissions_owner ON submissions (owner_id, created_at DESC, id DESC);
CREATE INDEX problems_page ON problems (created_at DESC, id DESC);
