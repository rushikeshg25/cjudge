ALTER TABLE submissions ADD CONSTRAINT submissions_attempts_nonnegative CHECK (attempts >= 0);
ALTER TABLE submissions ADD CONSTRAINT submissions_source_bound CHECK (octet_length(source) BETWEEN 1 AND 65536);
ALTER TABLE submissions ADD CONSTRAINT submissions_result_verdict CHECK (
    result IS NULL OR (
        jsonb_typeof(result) = 'object' AND result ? 'verdict' AND
        result->>'verdict' IN ('accepted','wrong_answer','compile_error','runtime_error',
          'time_limit_exceeded','memory_limit_exceeded','output_limit_exceeded','system_error')
    )
);
CREATE INDEX submissions_active_owner ON submissions(owner_id) WHERE state IN ('queued','running');

CREATE FUNCTION protect_terminal_submission() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    IF OLD.state = 'finished' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'terminal submissions are immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER submissions_terminal_guard BEFORE UPDATE ON submissions
FOR EACH ROW EXECUTE FUNCTION protect_terminal_submission();
