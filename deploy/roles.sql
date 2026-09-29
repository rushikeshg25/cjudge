-- Run once as the database owner AFTER migrations, in the dedicated cjudge DB.
-- These are group roles, not login users. Provision distinct LOGIN users through
-- your secret manager and grant one of these roles to each. No passwords here.
DO $$ BEGIN
  IF NOT EXISTS(SELECT FROM pg_roles WHERE rolname='cjudge_api') THEN CREATE ROLE cjudge_api NOLOGIN; END IF;
  IF NOT EXISTS(SELECT FROM pg_roles WHERE rolname='cjudge_worker') THEN CREATE ROLE cjudge_worker NOLOGIN; END IF;
  IF NOT EXISTS(SELECT FROM pg_roles WHERE rolname='cjudge_operator') THEN CREATE ROLE cjudge_operator NOLOGIN; END IF;
END $$;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO cjudge_api,cjudge_worker,cjudge_operator;
GRANT SELECT ON schema_migrations TO cjudge_api,cjudge_worker;
GRANT SELECT ON principals,api_keys TO cjudge_api;
GRANT SELECT(id,title,statement,checker,limits,created_at), INSERT ON problems TO cjudge_api;
GRANT SELECT ON submissions TO cjudge_api;
GRANT INSERT(id,owner_id,problem_id,language,source,idempotency_key,request_hash) ON submissions TO cjudge_api;
GRANT SELECT,INSERT,UPDATE ON rate_limits TO cjudge_api;
GRANT SELECT ON worker_heartbeats TO cjudge_api;
GRANT SELECT ON problems,submissions TO cjudge_worker;
GRANT UPDATE(state,attempts,lease_token,lease_until,available_at,result,updated_at) ON submissions TO cjudge_worker;
GRANT SELECT,INSERT,UPDATE ON worker_heartbeats TO cjudge_worker;
GRANT SELECT ON audit_events,principals,api_keys,submissions,worker_heartbeats TO cjudge_operator;
GRANT INSERT ON principals,api_keys TO cjudge_operator;
GRANT UPDATE(revoked) ON api_keys TO cjudge_operator;
-- No runtime role can DELETE records, change problem tests, mint credentials,
-- alter schema, or mutate audit history. Migrations use a separate owner login.
