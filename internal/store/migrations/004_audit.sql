ALTER TABLE problems ADD COLUMN author_id text REFERENCES principals(id);
CREATE TABLE audit_events (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    database_actor text NOT NULL,
    principal_id text,
    entity_type text NOT NULL,
    entity_id text NOT NULL,
    action text NOT NULL,
    details jsonb NOT NULL
);
CREATE INDEX audit_events_time ON audit_events(occurred_at);

-- Runtime roles cannot write this table directly. The schema is taken from the
-- trigger's table identity, never a client-controlled search_path.
CREATE FUNCTION capture_audit() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE record_json jsonb;
BEGIN
    record_json := to_jsonb(NEW);
    EXECUTE format('INSERT INTO %I.audit_events(database_actor,principal_id,entity_type,entity_id,action,details) VALUES($1,$2,$3,$4,$5,$6)', TG_TABLE_SCHEMA)
    USING session_user,
      COALESCE(record_json->>'owner_id',record_json->>'author_id',record_json->>'principal_id'),
      TG_TABLE_NAME,COALESCE(record_json->>'id',record_json->>'principal_id'),TG_OP,
      jsonb_strip_nulls(jsonb_build_object('state',record_json->>'state','attempts',record_json->'attempts','verdict',record_json->'result'->>'verdict','revoked',record_json->'revoked'));
    RETURN NULL;
END;
$$;
REVOKE ALL ON FUNCTION capture_audit() FROM PUBLIC;
CREATE TRIGGER problems_audit AFTER INSERT ON problems FOR EACH ROW EXECUTE FUNCTION capture_audit();
CREATE TRIGGER principals_audit AFTER INSERT ON principals FOR EACH ROW EXECUTE FUNCTION capture_audit();
CREATE TRIGGER keys_audit AFTER INSERT OR UPDATE ON api_keys FOR EACH ROW EXECUTE FUNCTION capture_audit();
CREATE TRIGGER submissions_audit_insert AFTER INSERT ON submissions FOR EACH ROW EXECUTE FUNCTION capture_audit();
CREATE TRIGGER submissions_audit_transition AFTER UPDATE ON submissions FOR EACH ROW
WHEN (OLD.state IS DISTINCT FROM NEW.state OR OLD.attempts IS DISTINCT FROM NEW.attempts)
EXECUTE FUNCTION capture_audit();
