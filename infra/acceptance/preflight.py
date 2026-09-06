#!/usr/bin/env python3
"""Fail-closed checks for the isolated acceptance runtime.

This reads environment files as configuration only. It never prints values,
starts containers, or creates business data.
"""
import json
import pathlib
import re
import shutil
import subprocess
import sys
import urllib.error
import urllib.request
from urllib.parse import urlsplit

root = pathlib.Path(__file__).resolve().parent


def read_env(path):
    """Read dotenv keys without expanding or echoing their values."""
    result = {}
    if not path.exists():
        return result
    for raw in path.read_text().splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[7:].lstrip()
        if "=" not in line:
            continue
        key, value = line.split("=", 1)
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        result[key.strip()] = value
    return result


def present(env, key):
    return bool(str(env.get(key, "")).strip())


missing = []
mismatches = []
compose_problems = []
companion_requirements = []
vault_probe = {"state": "not_checked"}
acceptance = read_env(root / ".env.acceptance")
core = read_env(root / ".env.core")
payments = read_env(root / ".env.payments")
development = acceptance.get("CREST_ENV") == "development"
payment_provider = acceptance.get("PAYMENT_PROVIDER", "http")
rail_required = payment_provider == "http"
if not development and payment_provider == "simulator":
    missing.append("development payment provider is forbidden outside development")

for key in (
    "POSTGRES_PASSWORD", "S3_ACCESS_KEY", "S3_SECRET_KEY", "VAULT_TOKEN",
    "CREST_SUBJECT_SALT", "CREST_INSTANCE_ID", "CREST_OPERATOR_PARTY_ID",
    "CREST_OIDC_ISSUER", "CREST_OIDC_JWKS_URL", "CREST_OIDC_AUDIENCE",
    "ESIGNET_URL", "ESIGNET_UI_URL", "CREST_AUTH_DOORS",
    "DEDI_URL", "DEDI_PUBLISHER_KEY", "CREST_SERVICE_PEERS_JSON",
):
    if not present(acceptance, key):
        missing.append(key)

if not re.fullmatch(r"did:crest:party:[0-9A-HJKMNP-TV-Z]{26}", acceptance.get("CREST_OPERATOR_PARTY_ID", "")):
    missing.append("CREST_OPERATOR_PARTY_ID must be a schema-valid Party DID")

for name, env in (("core", core), ("payments", payments)):
    for key in ("CREST_SERVICE_ID", "CREST_SERVICE_PRIVATE_KEY"):
        if not present(env, key):
            missing.append(name + ":" + key)

# A provider URL must be present, well-formed, and independent from mock
# services. The acceptance stack must exercise the configured providers.
for key in ("CREST_OIDC_ISSUER", "CREST_OIDC_JWKS_URL", "RAIL_URL", "ESIGNET_URL", "ESIGNET_UI_URL"):
    if key == "RAIL_URL" and not rail_required:
        continue
    value = acceptance.get(key, "").strip()
    if not value:
        missing.append(key + " must select a real provider")
        continue
    parsed = urlsplit(value)
    if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username or parsed.password:
        missing.append(key + " must be an HTTP service URL without credentials")
    elif "mock" in parsed.hostname.lower():
        missing.append(key + " must select a real provider")

if not ((present(acceptance, "SMTP_ADDR") and present(acceptance, "SMTP_FROM")) or present(acceptance, "NOTIFY_HTTP_URL")):
    missing.append("notification provider (SMTP_ADDR + SMTP_FROM, or NOTIFY_HTTP_URL)")
if present(acceptance, "SMTP_ADDR") != present(acceptance, "SMTP_FROM"):
    missing.append("SMTP_ADDR and SMTP_FROM must be configured together")
if not present(acceptance, "NOTIFY_ACK_BASE_URL"):
    missing.append("NOTIFY_ACK_BASE_URL")
if rail_required and acceptance.get("PAYMENT_SUBSCRIBER_ENABLED", "").strip().lower() == "true" and not present(acceptance, "RAIL_URL"):
    missing.append("RAIL_URL for the enabled payment subscriber")
doors = [door.strip() for door in acceptance.get("CREST_AUTH_DOORS", "").split(",") if door.strip()]
if not doors:
    missing.append("CREST_AUTH_DOORS (at least one exact browser door origin)")
else:
    for door in doors:
        parsed = urlsplit(door)
        if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username or parsed.password:
            missing.append("CREST_AUTH_DOORS contains an invalid browser origin")

# Provider changes must be reflected in every service that consumes them.
# Payments owns the rail; core owns DeDi, status, Vault and S3. Values are
# compared internally and only mismatching key names are reported.
for key, consumers in {
    "CREST_OIDC_ISSUER": ("core", "payments"),
    "CREST_OIDC_JWKS_URL": ("core", "payments"),
    "CREST_OIDC_AUDIENCE": ("core", "payments"),
    "ESIGNET_URL": ("core",),
    "ESIGNET_UI_URL": ("core",),
    "CREST_AUTH_DOORS": ("core",),
    "CREST_INSTANCE_ID": ("core", "payments"),
    "CREST_OPERATOR_PARTY_ID": ("core", "payments"),
    "CREST_SUBJECT_SALT": ("core", "payments"),
    "CREST_SERVICE_PEERS_JSON": ("core", "payments"),
    "RAIL_URL": ("payments",),
    "PAYMENT_PROVIDER": ("payments",),
    "CREST_ENV": ("core", "payments"),
    "DEDI_URL": ("core",),
    "DEDI_NAMESPACE": ("core",),
    "DEDI_KEY_ID": ("core",),
    "DEDI_PUBLISHER_KEY": ("core",),
    "STATUS_LIST_URL": ("core",),
    "S3_ACCESS_KEY": ("core",),
    "S3_SECRET_KEY": ("core",),
    "VAULT_TOKEN": ("core",),
    "PAYMENT_SUBSCRIBER_ENABLED": ("core",),
    "SMTP_ADDR": ("core",),
    "SMTP_FROM": ("core",),
    "NOTIFY_HTTP_URL": ("core",),
    "NOTIFY_ACK_BASE_URL": ("core",),
}.items():
    expected = acceptance.get(key, "")
    for name, env in (("core", core), ("payments", payments)):
        if name not in consumers:
            continue
        if env.get(key, "") != expected:
            mismatches.append(key + " differs between acceptance and " + name)

# Payments has its own database and rail. It must not receive core custody,
# object-store, transparency-log, or root database credentials.
for key in payments:
    upper = key.upper()
    if upper.startswith(("VAULT_", "S3_", "DEDI_")) or upper in {
        "POSTGRES_PASSWORD", "POSTGRES_USER", "POSTGRES_DB", "PGPASSWORD",
    }:
        compose_problems.append("payments env contains forbidden key " + key)
if "DATABASE_URL" not in payments:
    compose_problems.append("payments env must define its own DATABASE_URL")
elif re.search(r"(?:postgres|postgresql)://(?:root|postgres)(?:[:@/]|$)", payments["DATABASE_URL"], re.I):
    compose_problems.append("payments DATABASE_URL uses a root database user")

# Check the acceptance graph and the runtime anchor without loading YAML or
# exposing interpolated values.
compose = (root / "compose.yml").read_text()
services_section = compose.split("services:", 1)[1] if "services:" in compose else ""
service_names = set(re.findall(r"^  ([A-Za-z0-9_-]+):\s*$", services_section, re.M))
for name in ("postgres", "dedi-postgres", "dedi", "objectstore", "vault", "core", "payments", "apps"):
    if name not in service_names:
        compose_problems.append("acceptance compose omits service " + name)
if "env_file: .env.core" not in compose or "env_file: .env.payments" not in compose:
    compose_problems.append("core/payments env_file wiring is incomplete")
anchor = compose.split("x-runtime:", 1)[1].split("services:", 1)[0] if "x-runtime:" in compose and "services:" in compose else ""
for key in ("S3_ENDPOINT", "S3_BUCKET", "VAULT_ADDR", "VAULT_SECRET_PATH", "VAULT_TOKEN", "DEDI_PUBLISHER_KEY"):
    if re.search(r"^\s+" + re.escape(key) + r":", anchor, re.M):
        compose_problems.append("payments inherits core-only runtime key " + key)
core_block = compose.split("  core:", 1)[1].split("  payments:", 1)[0] if "  core:" in compose and "  payments:" in compose else ""
for key in ("S3_ENDPOINT", "S3_BUCKET", "VAULT_ADDR", "VAULT_SECRET_PATH"):
    if not re.search(r"^\s+" + re.escape(key) + r":", core_block, re.M):
        compose_problems.append("core does not declare " + key + " in its environment")

if not (root.parent.parent / "frontend" / "dist-site" / "index.html").is_file():
    companion_requirements.append("build frontend/dist-site/index.html before starting the apps container")
for filename in ("private/dedid.key", "private/s3.json"):
    if not (root / filename).is_file():
        companion_requirements.append("acceptance compose requires " + filename)

# Vault is intentionally not auto-initialized: initialization and unseal are a
# deployment ceremony. Once the operator has completed it, prove both that the
# local listener is unsealed and that the configured token can read the issuer
# secret. Response bodies are consumed only for shape checks and never printed.
vault_url = "http://127.0.0.1:59300"
try:
    with urllib.request.urlopen(vault_url + "/v1/sys/health", timeout=2) as response:
        health_status = response.status
except urllib.error.HTTPError as error:
    health_status = error.code
except (urllib.error.URLError, TimeoutError, OSError):
    health_status = 0
if health_status == 200:
    vault_probe["state"] = "initialized_and_unsealed"
    secret_path = core.get("VAULT_SECRET_PATH", "secret/data/crest/issuer") or "secret/data/crest/issuer"
    request = urllib.request.Request(
        vault_url + "/v1/" + secret_path.lstrip("/"),
        headers={"X-Vault-Token": acceptance.get("VAULT_TOKEN", "")},
    )
    try:
        with urllib.request.urlopen(request, timeout=2) as response:
            payload = json.loads(response.read(1024 * 1024))
            data = payload.get("data", {}) if isinstance(payload, dict) else {}
            nested = data.get("data", {}) if isinstance(data, dict) else {}
            if response.status == 200 and isinstance(nested, dict) and (present(nested, "issuer_seed") or present(nested, "privateKey")):
                vault_probe["state"] = "issuer_secret_readable"
                vault_probe["fields"] = sorted(nested.keys())
            else:
                vault_probe["state"] = "issuer_secret_missing"
    except urllib.error.HTTPError as error:
        vault_probe["state"] = "issuer_secret_forbidden" if error.code in (401, 403) else "issuer_secret_unavailable"
    except (urllib.error.URLError, TimeoutError, OSError, ValueError):
        vault_probe["state"] = "issuer_secret_unavailable"
elif health_status in (501, 472, 473, 503):
    vault_probe["state"] = "sealed_or_uninitialized"
else:
    vault_probe["state"] = "listener_unavailable"
if vault_probe["state"] != "issuer_secret_readable":
    companion_requirements.append("Vault must be initialized/unsealed and the configured token must read the issuer secret before core starts (probe: " + vault_probe["state"] + ")")
elif not present(core, "ESIGNET_CLIENT_KEY"):
    fields = set(vault_probe.get("fields", []))
    if "esignet_client_key" not in fields:
        companion_requirements.append("ESIGNET_CLIENT_KEY must be supplied through core's environment or the Vault issuer secret")

# The acceptance graph deliberately runs CREST core, payments, storage, Vault,
# DeDi, Postgres and the browser apps. MOSIP companions are optional and are
# listed explicitly so their omission cannot be mistaken for a complete MOSIP
# provider flow.
optional_companions = [
    {"service": "eSignet", "status": "external_configured_not_probed" if present(core, "ESIGNET_URL") else "not_configured", "neededFor": "real browser OIDC login; acceptance must configure an external ESIGNET_URL/UI, client key, browser door, and matching CREST_OIDC issuer/JWKS"},
    {"service": "Certify", "status": "outside_acceptance_compose_not_probed", "neededFor": "MOSIP Certify credential issuance integration, when that provider flow is selected"},
    {"service": "Mimoto", "status": "outside_acceptance_compose_not_probed", "neededFor": "MOSIP wallet enrollment/storage integration"},
    {"service": "Inji Verify", "status": "outside_acceptance_compose_not_probed", "neededFor": "MOSIP's upstream verifier UI/API; CREST's own verify app remains separate"},
]

# Compose config is a read-only interpolation/YAML check. Suppress all
# command output because it may contain expanded dotenv values.
docker = shutil.which("docker")
if docker:
    result = subprocess.run(
        [docker, "compose", "--env-file", ".env.acceptance", "-f", "compose.yml", "config", "--quiet"],
        cwd=root, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False,
    )
    if result.returncode:
        compose_problems.append("docker compose config rejected infra/acceptance/compose.yml")
else:
    compose_problems.append("docker executable unavailable for compose config validation")

report = {
    "validationScope": "development-integration" if development else "provider-acceptance",
    "productionProviderAcceptance": False,
    "identityProvider": "official eSignet with its development identity plugin" if development else "configured provider",
    "selectedEvidenceAdapter": "csv-batch@1",
    "selectedPaymentProvider": payment_provider,
    "readyForProviderValidation": not (missing or mismatches or compose_problems or companion_requirements),
    "missingConfiguration": sorted(set(missing)),
    "runtimeMismatches": sorted(set(mismatches)),
    "composeProblems": sorted(set(compose_problems)),
    "companionRequirements": sorted(set(companion_requirements)),
    "vaultProbe": vault_probe,
    "optionalMOSIPCompanions": optional_companions,
    "businessDataCreatedByPreflight": False,
}
print(json.dumps(report, indent=2))
sys.exit(1 if not report["readyForProviderValidation"] else 0)
