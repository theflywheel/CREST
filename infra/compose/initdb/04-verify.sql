-- Inji Verify's database (#155 phase C).
--
-- The service migrates its own tables on boot; all it needs from us is the
-- database and the schema its DATABASE_SCHEMA points at, owned by the same
-- non-superuser role everything else runs as. Mirrors the deployed service's
-- DATABASE_* variables (docs/DEPLOYMENT.md).
CREATE DATABASE inji_verify OWNER crest ENCODING 'UTF8';

\c inji_verify crest
CREATE SCHEMA IF NOT EXISTS verify AUTHORIZATION crest;
