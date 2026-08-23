-- Inji Verify's database.
--
-- Vendored from inji-verify v0.16.0 (docker-compose/db-init/init.sql), with the
-- database created explicitly and owned by the CREST role rather than by the
-- superuser. Upstream's compose lets the postgres image create it as `postgres`,
-- which is fine for a laptop and not for a deployment where nothing should be
-- able to connect as a superuser.
--
-- Its own database, not a schema inside `crest`: a verifier holds presentation
-- state about credentials it was shown, and that must not sit where a CREST
-- migration can reach it.

CREATE DATABASE inji_verify OWNER crest ENCODING 'UTF8';

\c inji_verify crest

CREATE SCHEMA IF NOT EXISTS verify;

CREATE TABLE IF NOT EXISTS verify.authorization_request_details (
    request_id character varying(40) NOT NULL,
    transaction_id character varying(40) NOT NULL,
    authorization_details text NOT NULL,
    expires_at bigint NOT NULL
);

CREATE TABLE IF NOT EXISTS verify.presentation_definition(
    id character varying(36) NOT NULL,
    input_descriptors jsonb NOT NULL,
    name character varying(500),
    purpose character varying(500),
    vp_format text,
    submission_requirements text
);

CREATE TABLE IF NOT EXISTS verify.vc_submission(
    transaction_id character varying(40) NOT NULL,
    vc text NOT NULL
);

CREATE TABLE IF NOT EXISTS verify.vp_submission(
    request_id character varying(40) NOT NULL,
    vp_token VARCHAR NOT NULL,
    presentation_submission text NOT NULL,
    error character varying(100) NULL,
    error_description character varying(200) NULL
);
