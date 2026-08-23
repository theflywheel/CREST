-- Inji Certify's database.
--
-- Vendored from inji-certify v0.14.0
-- (docker-compose/docker-compose-injistack/certify_init.sql), with three
-- changes: the database and schema are owned by the CREST role rather than by
-- the superuser, upstream's FarmerCredential row is gone, and a
-- WorkEventCredential row takes its place.
--
-- Separate database, same server as `crest`. Certify's keymanager owns signing
-- keys and its ledger records what was issued; a CREST migration must not be
-- able to reach either. Keeping them in one database would make that only a
-- matter of someone writing the wrong `search_path`.
--
-- Regenerating: fetch that file at the tag in .env.example
-- (INJI_CERTIFY_IMAGE), drop its CREATE DATABASE block and its
-- FarmerCredential INSERT, and re-apply the three changes above.

CREATE DATABASE inji_certify OWNER crest ENCODING 'UTF8';

\c inji_certify crest
DROP SCHEMA IF EXISTS certify CASCADE;
CREATE SCHEMA certify;
ALTER SCHEMA certify OWNER TO crest;
ALTER DATABASE inji_certify SET search_path TO certify,pg_catalog,public;

--- keymanager specific DB changes ---
CREATE TABLE certify.key_alias(
                                  id character varying(36) NOT NULL,
                                  app_id character varying(36) NOT NULL,
                                  ref_id character varying(128),
                                  key_gen_dtimes timestamp,
                                  key_expire_dtimes timestamp,
                                  status_code character varying(36),
                                  lang_code character varying(3),
                                  cr_by character varying(256) NOT NULL,
                                  cr_dtimes timestamp NOT NULL,
                                  upd_by character varying(256),
                                  upd_dtimes timestamp,
                                  is_deleted boolean DEFAULT FALSE,
                                  del_dtimes timestamp,
                                  cert_thumbprint character varying(100),
                                  uni_ident character varying(50),
                                  CONSTRAINT pk_keymals_id PRIMARY KEY (id),
                                  CONSTRAINT uni_ident_const UNIQUE (uni_ident)
);

CREATE TABLE certify.key_policy_def(
                                       app_id character varying(36) NOT NULL,
                                       key_validity_duration smallint,
                                       is_active boolean NOT NULL,
                                       pre_expire_days smallint,
                                       access_allowed character varying(1024),
                                       cr_by character varying(256) NOT NULL,
                                       cr_dtimes timestamp NOT NULL,
                                       upd_by character varying(256),
                                       upd_dtimes timestamp,
                                       is_deleted boolean DEFAULT FALSE,
                                       del_dtimes timestamp,
                                       CONSTRAINT pk_keypdef_id PRIMARY KEY (app_id)
);

CREATE TABLE certify.key_store(
                                  id character varying(36) NOT NULL,
                                  master_key character varying(36) NOT NULL,
                                  private_key character varying(2500) NOT NULL,
                                  certificate_data character varying NOT NULL,
                                  cr_by character varying(256) NOT NULL,
                                  cr_dtimes timestamp NOT NULL,
                                  upd_by character varying(256),
                                  upd_dtimes timestamp,
                                  is_deleted boolean DEFAULT FALSE,
                                  del_dtimes timestamp,
                                  CONSTRAINT pk_keystr_id PRIMARY KEY (id)
);

CREATE TABLE certify.ca_cert_store(
    cert_id character varying(36) NOT NULL,
    cert_subject character varying(500) NOT NULL,
    cert_issuer character varying(500) NOT NULL,
    issuer_id character varying(36) NOT NULL,
    cert_not_before timestamp,
    cert_not_after timestamp,
    crl_uri character varying(120),
    cert_data character varying,
    cert_thumbprint character varying(100),
    cert_serial_no character varying(50),
    partner_domain character varying(36),
    cr_by character varying(256),
    cr_dtimes timestamp,
    upd_by character varying(256),
    upd_dtimes timestamp,
    is_deleted boolean DEFAULT FALSE,
    del_dtimes timestamp,
    ca_cert_type character varying(25),
    CONSTRAINT pk_cacs_id PRIMARY KEY (cert_id),
    CONSTRAINT cert_thumbprint_unique UNIQUE (cert_thumbprint,partner_domain)

);

CREATE TABLE certify.rendering_template (
                                    id varchar(128) NOT NULL,
                                    template VARCHAR NOT NULL,
                                    cr_dtimes timestamp NOT NULL,
                                    upd_dtimes timestamp,
                                    CONSTRAINT pk_svgtmp_id PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS certify.credential_config (
    credential_config_key_id VARCHAR(2048) NOT NULL UNIQUE,
    config_id VARCHAR(255) NOT NULL,
    status VARCHAR(255),
    vc_template VARCHAR,
    doctype VARCHAR,
    sd_jwt_vct VARCHAR,
    context VARCHAR,
    credential_type VARCHAR,
    credential_format VARCHAR(255) NOT NULL,
    did_url VARCHAR,
    key_manager_app_id VARCHAR(36),
    key_manager_ref_id VARCHAR(128),
    signature_algo VARCHAR(36),
    signature_crypto_suite VARCHAR(128),
    sd_claim VARCHAR,
    display JSONB NOT NULL,
    display_order TEXT[] NOT NULL,
    scope VARCHAR(255) NOT NULL,
    cryptographic_binding_methods_supported TEXT[] NOT NULL,
    credential_signing_alg_values_supported TEXT[] NOT NULL,
    proof_types_supported JSONB NOT NULL,
    credential_subject JSONB,
    sd_jwt_claims JSONB,
    mso_mdoc_claims JSONB,
    plugin_configurations JSONB,
    credential_status_purpose TEXT[],
    qr_settings JSONB,
    qr_signature_algo TEXT,
    cr_dtimes TIMESTAMP NOT NULL,
    upd_dtimes TIMESTAMP,
    CONSTRAINT pk_config_id PRIMARY KEY (config_id)
);

CREATE UNIQUE INDEX idx_credential_config_type_context_unique
ON certify.credential_config(credential_type, context, credential_format)
WHERE credential_type IS NOT NULL AND credential_type <> ''
AND context IS NOT NULL AND context <> '';

CREATE UNIQUE INDEX idx_credential_config_sd_jwt_vct_unique
ON certify.credential_config(sd_jwt_vct, credential_format)
WHERE sd_jwt_vct IS NOT NULL and sd_jwt_vct <> '';

CREATE UNIQUE INDEX idx_credential_config_doctype_unique
ON certify.credential_config(doctype, credential_format)
WHERE doctype IS NOT NULL and doctype <> '';


INSERT INTO certify.key_policy_def(APP_ID,KEY_VALIDITY_DURATION,PRE_EXPIRE_DAYS,ACCESS_ALLOWED,IS_ACTIVE,CR_BY,CR_DTIMES) VALUES('ROOT', 2920, 1125, 'NA', true, 'mosipadmin', now());
INSERT INTO certify.key_policy_def(APP_ID,KEY_VALIDITY_DURATION,PRE_EXPIRE_DAYS,ACCESS_ALLOWED,IS_ACTIVE,CR_BY,CR_DTIMES) VALUES('CERTIFY_SERVICE', 1095, 60, 'NA', true, 'mosipadmin', now());
INSERT INTO certify.key_policy_def(APP_ID,KEY_VALIDITY_DURATION,PRE_EXPIRE_DAYS,ACCESS_ALLOWED,IS_ACTIVE,CR_BY,CR_DTIMES) VALUES('CERTIFY_PARTNER', 1095, 60, 'NA', true, 'mosipadmin', now());
INSERT INTO certify.key_policy_def(APP_ID,KEY_VALIDITY_DURATION,PRE_EXPIRE_DAYS,ACCESS_ALLOWED,IS_ACTIVE,CR_BY,CR_DTIMES) VALUES('CERTIFY_VC_SIGN_RSA', 1095, 60, 'NA', true, 'mosipadmin', now());
INSERT INTO certify.key_policy_def(APP_ID,KEY_VALIDITY_DURATION,PRE_EXPIRE_DAYS,ACCESS_ALLOWED,IS_ACTIVE,CR_BY,CR_DTIMES) VALUES('CERTIFY_VC_SIGN_ED25519', 1095, 60, 'NA', true, 'mosipadmin', now());
INSERT INTO certify.key_policy_def(APP_ID,KEY_VALIDITY_DURATION,PRE_EXPIRE_DAYS,ACCESS_ALLOWED,IS_ACTIVE,CR_BY,CR_DTIMES) VALUES('BASE', 1095, 60, 'NA', true, 'mosipadmin', now());
INSERT INTO certify.key_policy_def(APP_ID,KEY_VALIDITY_DURATION,PRE_EXPIRE_DAYS,ACCESS_ALLOWED,IS_ACTIVE,CR_BY,CR_DTIMES) VALUES('CERTIFY_VC_SIGN_EC_K1', 1095, 60, 'NA', true, 'mosipadmin', now());
INSERT INTO certify.key_policy_def(APP_ID,KEY_VALIDITY_DURATION,PRE_EXPIRE_DAYS,ACCESS_ALLOWED,IS_ACTIVE,CR_BY,CR_DTIMES) VALUES('CERTIFY_VC_SIGN_EC_R1', 1095, 60, 'NA', true, 'mosipadmin', now());

CREATE TYPE credential_status_enum AS ENUM ('AVAILABLE', 'FULL');

-- Create status_list_credential table
CREATE TABLE certify.status_list_credential (
    id VARCHAR(255) PRIMARY KEY,          -- The unique ID (URL/DID/URN) extracted from the VC's 'id' field.
    vc_document VARCHAR NOT NULL,           -- Stores the entire Verifiable Credential JSON document.
    credential_type VARCHAR(100) NOT NULL, -- Type of the status list (e.g., 'StatusList2021Credential')
    status_purpose VARCHAR(100),             -- Intended purpose of this list within the system (e.g., 'revocation', 'suspension', 'general'). NULLABLE.
    capacity BIGINT,                        --- length of status list
    credential_status credential_status_enum, -- Use the created ENUM type here
    cr_dtimes timestamp NOT NULL default now(),
    upd_dtimes timestamp                    -- When this VC record was last updated in the system
);


CREATE INDEX IF NOT EXISTS idx_slc_status_purpose ON certify.status_list_credential(status_purpose);
CREATE INDEX IF NOT EXISTS idx_slc_credential_type ON certify.status_list_credential(credential_type);
CREATE INDEX IF NOT EXISTS idx_slc_credential_status ON certify.status_list_credential(credential_status);
CREATE INDEX IF NOT EXISTS idx_slc_cr_dtimes ON certify.status_list_credential(cr_dtimes);

-- Create the ledger table
CREATE TABLE certify.ledger (
    id SERIAL PRIMARY KEY,                          -- Auto-incrementing serial primary key
    credential_id VARCHAR(255),            -- Unique ID of the Verifiable Credential WHOSE STATUS IS BEING TRACKED
    issuer_id VARCHAR(255) NOT NULL,                -- Issuer of the TRACKED credential
    issuance_date TIMESTAMP NOT NULL,                -- Issuance date of the TRACKED credential
    expiration_date TIMESTAMP,                    -- Expiration date of the TRACKED credential, if any
    credential_type VARCHAR(100) NOT NULL,          -- Type of the TRACKED credential (e.g., 'VerifiableId')
    indexed_attributes JSONB,                       -- Optional searchable attributes from the TRACKED credential
    credential_status_details JSONB NOT NULL DEFAULT '[]'::jsonb,    -- Stores a list of status objects for this credential, defaults to an empty array.
    cr_dtimes TIMESTAMP NOT NULL DEFAULT NOW(),     -- Creation timestamp of this ledger entry for the tracked credential

    -- Constraints
    CONSTRAINT uq_ledger_tracked_credential_id UNIQUE (credential_id), -- Ensure tracked credential_id is unique
    CONSTRAINT ensure_credential_status_details_is_array CHECK (jsonb_typeof(credential_status_details) = 'array') -- Ensure it's always a JSON array
);


CREATE INDEX IF NOT EXISTS idx_ledger_credential_id ON certify.ledger(credential_id);
CREATE INDEX IF NOT EXISTS idx_ledger_issuer_id ON certify.ledger(issuer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_credential_type ON certify.ledger(credential_type);
CREATE INDEX IF NOT EXISTS idx_ledger_issuance_date ON certify.ledger(issuance_date);
CREATE INDEX IF NOT EXISTS idx_ledger_expiration_date ON certify.ledger(expiration_date);
CREATE INDEX IF NOT EXISTS idx_ledger_cr_dtimes ON certify.ledger(cr_dtimes);
CREATE INDEX IF NOT EXISTS idx_gin_ledger_indexed_attrs ON certify.ledger USING GIN (indexed_attributes);
CREATE INDEX IF NOT EXISTS idx_gin_ledger_status_details ON certify.ledger USING GIN (credential_status_details);

CREATE TABLE IF NOT EXISTS certify.credential_status_transaction (
    transaction_log_id SERIAL PRIMARY KEY,        -- Unique ID for this transaction log entry
    credential_id VARCHAR(255), -- The ID of the credential this transaction pertains to (should exist in ledger.credential_id)
    status_purpose VARCHAR(100),                  -- The purpose of this status update
    status_value boolean,                         -- The status value (true/false)
    status_list_credential_id VARCHAR(255),       -- The ID of the status list credential involved, if any
    status_list_index BIGINT,                     -- The index on the status list, if any
    cr_dtimes TIMESTAMP NOT NULL DEFAULT NOW(),   -- Creation timestamp
    processed_dtimes TIMESTAMP,                      -- Timestamp when processed by status list batch job
    is_processed BOOLEAN NOT NULL DEFAULT FALSE   -- Indicates if processed by status list batch job
);

CREATE INDEX IF NOT EXISTS idx_cst_is_processed_created ON certify.credential_status_transaction (is_processed, cr_dtimes);

CREATE TABLE certify.status_list_available_indices (
    id SERIAL PRIMARY KEY,                         -- Serial primary key
    status_list_credential_id VARCHAR(255) NOT NULL, -- References status_list_credential.id
    list_index BIGINT NOT NULL,                    -- The numerical index within the status list
    is_assigned BOOLEAN NOT NULL DEFAULT FALSE,   -- Flag indicating if this index has been assigned
    cr_dtimes TIMESTAMP NOT NULL DEFAULT NOW(),   -- Creation timestamp
    upd_dtimes TIMESTAMP,                          -- Update timestamp

    -- Foreign key constraint
    CONSTRAINT fk_status_list_credential
        FOREIGN KEY(status_list_credential_id)
        REFERENCES certify.status_list_credential(id)
        ON DELETE CASCADE -- If a status list credential is deleted, its available index entries are also deleted.
        ON UPDATE CASCADE, -- If the ID of a status list credential changes, update it here too.

    -- Unique constraint to ensure each index within a list is represented only once
    CONSTRAINT uq_list_id_and_index
        UNIQUE (status_list_credential_id, list_index)
);

CREATE INDEX IF NOT EXISTS idx_sla_available_indices
    ON certify.status_list_available_indices (status_list_credential_id, is_assigned, list_index)
    WHERE is_assigned = FALSE;

-- Additional indexes for performance
CREATE INDEX IF NOT EXISTS idx_sla_status_list_credential_id ON certify.status_list_available_indices(status_list_credential_id);
CREATE INDEX IF NOT EXISTS idx_sla_is_assigned ON certify.status_list_available_indices(is_assigned);
CREATE INDEX IF NOT EXISTS idx_sla_list_index ON certify.status_list_available_indices(list_index);
CREATE INDEX IF NOT EXISTS idx_sla_cr_dtimes ON certify.status_list_available_indices(cr_dtimes);

CREATE TABLE IF NOT EXISTS certify.shedlock (
  name VARCHAR(64),
  lock_until TIMESTAMPTZ(3) NOT NULL,
  locked_at TIMESTAMPTZ(3) NOT NULL,
  locked_by VARCHAR(255) NOT NULL,
  PRIMARY KEY (name)
);

-- This script creates the iar_session table for Interactive Authorization Request functionality

CREATE TABLE IF NOT EXISTS certify.iar_session (
                                                   id SERIAL PRIMARY KEY,
                                                   auth_session VARCHAR(128) NOT NULL UNIQUE,
    transaction_id VARCHAR(64) NOT NULL,
    request_id VARCHAR(64),
    verify_nonce VARCHAR(64),
    expires_at TIMESTAMP NOT NULL,
    client_id VARCHAR(128),
    scope VARCHAR(128),
    authorization_code VARCHAR(128) UNIQUE,
    response_uri VARCHAR(512),
    code_challenge VARCHAR(128),
    code_challenge_method VARCHAR(10),
    code_issued_at TIMESTAMP,
    is_code_used BOOLEAN NOT NULL DEFAULT FALSE,
    code_used_at TIMESTAMP,
    cr_dtimes TIMESTAMP NOT NULL DEFAULT NOW(),
    identity_data TEXT
    );

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_iar_session_auth_session ON certify.iar_session(auth_session);
CREATE INDEX IF NOT EXISTS idx_iar_session_authorization_code ON certify.iar_session(authorization_code);
CREATE INDEX IF NOT EXISTS idx_iar_session_request_id ON certify.iar_session(request_id);
CREATE INDEX IF NOT EXISTS idx_iar_session_expires_at ON certify.iar_session(expires_at);
CREATE INDEX IF NOT EXISTS idx_iar_session_authorization_code_used ON certify.iar_session(authorization_code, is_code_used) WHERE authorization_code IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_iar_session_scope ON certify.iar_session(scope);
CREATE INDEX IF NOT EXISTS idx_iar_session_transaction_id ON certify.iar_session(transaction_id);

-- ── The WorkEventCredential ──────────────────────────────────────────────────
--
-- Certify keeps credential configuration in the database rather than in a
-- properties file, so this row is the credential's definition: its type, its
-- context, what it is signed with, and which plugin fills it in.
--
-- The template is base64 of a JSON document with ${placeholders} the data
-- provider substitutes. Decode it before changing it:
--
--   psql -At -c "select vc_template from certify.credential_config \
--     where credential_config_key_id='WorkEventCredential'" | base64 -d
--
-- What is NOT in the template is the part worth reviewing: no tier, no national
-- identifier, no biometric, no name. A tier in a signed offline artefact is a
-- judgement frozen at issuance that a verifier can no longer make for itself,
-- and that cannot be raised when the worker's identity assurance improves.
-- The facts are there instead, under `provenance`, and the tier is derived from
-- them by whoever is asking.
INSERT INTO certify.credential_config (
    credential_config_key_id, config_id, status, vc_template,
    doctype, sd_jwt_vct, context, credential_type, credential_format,
    did_url, key_manager_app_id, key_manager_ref_id, signature_algo,
    signature_crypto_suite, sd_claim, display, display_order, scope,
    cryptographic_binding_methods_supported, credential_signing_alg_values_supported,
    proof_types_supported, credential_subject, mso_mdoc_claims,
    plugin_configurations, credential_status_purpose, cr_dtimes, upd_dtimes
) VALUES (
    'WorkEventCredential',
    gen_random_uuid()::VARCHAR(255),
    'active',
    'ewogICJAY29udGV4dCI6IFsKICAgICJodHRwczovL3d3dy53My5vcmcvMjAxOC9jcmVkZW50aWFscy92MSIsCiAgICAiaHR0cHM6Ly9jcmVzdC50aGVmbHl3aGVlbC5pbi9jb250ZXh0cy93b3JrLWV2ZW50L3YxIgogIF0sCiAgImlzc3VlciI6ICIke19pc3N1ZXJ9IiwKICAidHlwZSI6IFsiVmVyaWZpYWJsZUNyZWRlbnRpYWwiLCAiV29ya0V2ZW50Q3JlZGVudGlhbCJdLAogICJpc3N1YW5jZURhdGUiOiAiJHt2YWxpZEZyb219IiwKICAiZXhwaXJhdGlvbkRhdGUiOiAiJHt2YWxpZFVudGlsfSIsCiAgImNyZWRlbnRpYWxTdWJqZWN0IjogewogICAgImlkIjogIiR7X2hvbGRlcklkfSIsCiAgICAidW5pdElkIjogIiR7dW5pdElkfSIsCiAgICAiY2xhaW1JZCI6ICIke2NsYWltSWR9IiwKICAgICJhY3Rpdml0eSI6ICIke2FjdGl2aXR5fSIsCiAgICAiZGVmaW5pdGlvbiI6IHsKICAgICAgInJlZiI6ICIke2RlZmluaXRpb25SZWZ9IiwKICAgICAgInZlcnNpb24iOiAiJHtkZWZpbml0aW9uVmVyc2lvbn0iCiAgICB9LAogICAgInBlcmlvZCI6IHsKICAgICAgInN0YXJ0IjogIiR7cGVyaW9kU3RhcnR9IiwKICAgICAgImVuZCI6ICIke3BlcmlvZEVuZH0iCiAgICB9LAogICAgIm91dGNvbWUiOiB7CiAgICAgICJ2YWx1ZSI6ICR7b3V0Y29tZVZhbHVlfSwKICAgICAgInVuaXQiOiAiJHtvdXRjb21lVW5pdH0iCiAgICB9LAogICAgImNvbnRleHRSZWYiOiAiJHtjb250ZXh0UmVmfSIsCiAgICAiaXNzdWVyT3JnIjogIiR7aXNzdWVyT3JnfSIsCiAgICAicHJvdmVuYW5jZSI6IHsKICAgICAgInNvdXJjZUNsYXNzIjogIiR7c291cmNlQ2xhc3N9IiwKICAgICAgImNhcHR1cmVNZXRob2QiOiAiJHtjYXB0dXJlTWV0aG9kfSIsCiAgICAgICJhZGFwdGVyUmVmIjogIiR7YWRhcHRlclJlZn0iLAogICAgICAicmVjZWl2ZWRBdCI6ICIke3JlY2VpdmVkQXR9IiwKICAgICAgInNvdXJjZUV4cG9zdXJlIjogIiR7c291cmNlRXhwb3N1cmV9IgogICAgfQogIH0KfQ==',
    NULL,
    NULL,
    'https://www.w3.org/2018/credentials/v1',
    -- Order matters, though it should not: Certify looks a config up by the
    -- comma-joined type list exactly as the wallet sent it, so a request for
    -- ["VerifiableCredential","WorkEventCredential"] — the conventional W3C
    -- order — does not match a row written the other way round. The failure
    -- names ERROR_SIGNING_QR_DATA, which is not what went wrong.
    'VerifiableCredential,WorkEventCredential',
    'ldp_vc',
    -- Overwritten at deploy time to match the deployment's own domain: did:web
    -- maps host and path, so a DID minted for localhost does not resolve
    -- anywhere else. See infra/certify/certify-start.sh.
    'did:web:localhost%3A58090:v1:certify',
    'CERTIFY_VC_SIGN_ED25519',
    'ED25519_SIGN',
    'EdDSA',
    -- eddsa-jcs-2022: JCS canonicalises the JSON itself, so signing never has to
    -- dereference @context over the network, and CREST's own signer already
    -- emits this suite. §5 of the blueprint still names the JSON-LD suite as
    -- the default — that disagreement is #60.
    'eddsa-jcs-2022',
    NULL,
    '[{"name": "Work Event", "locale": "en", "background_color": "#0f3d2e", "text_color": "#FFFFFF"}]'::JSONB,
    ARRAY['activity', 'period', 'outcome', 'provenance'],
    'crest_work_event_vc_ldp',
    ARRAY['did:jwk'],
    ARRAY['eddsa-jcs-2022'],
    -- The holder's proof-of-possession algorithms. Ed25519 is here because the
    -- wallet key that a credential is bound to should be the same shape as the
    -- issuer key; RS256 and ES256 stay for wallets that cannot mint one.
    -- Certify rejects anything not in this list with `proof_header_invalid_alg`,
    -- which surfaces to the wallet as a parse error rather than as "that
    -- algorithm is not allowed".
    '{"jwt": {"proof_signing_alg_values_supported": ["Ed25519", "EdDSA", "RS256", "ES256"]}}'::JSONB,
    '{"activity": {"display": [{"name": "Activity", "locale": "en"}]}, "period": {"display": [{"name": "Period", "locale": "en"}]}, "outcome": {"display": [{"name": "Outcome", "locale": "en"}]}, "provenance": {"display": [{"name": "Where this record came from", "locale": "en"}]}}'::JSONB,
    NULL,
    '[{"mosip.certify.mock.data-provider.csv.identifier-column": "individualId", "mosip.certify.mock.data-provider.csv.data-columns": "individualId,unitId,claimId,definitionRef,definitionVersion,activity,periodStart,periodEnd,outcomeValue,outcomeUnit,contextRef,sourceClass,captureMethod,adapterRef,receivedAt,sourceExposure,issuerOrg", "mosip.certify.mock.data-provider.csv-registry-uri": "/home/inji/config/work_events.csv"}]'::JSONB,
    -- A credential whose claim is later disputed has to be revocable, and a
    -- status list is the only central fact CREST keeps about an issued
    -- credential (§3, §9). What a dispute actually does to it is undecided —
    -- that is #58, and this column is what makes either answer implementable.
    ARRAY['revocation'],
    NOW(),
    NULL
);
