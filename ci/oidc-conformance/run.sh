#!/usr/bin/env bash
# Runs the OpenID Foundation conformance suite (Config OP and Basic OP plans)
# against a local turna. Needs docker, psql, python3 and Go.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(git rev-parse --show-toplevel)
SUITE=https://localhost:8443
ADMIN=http://127.0.0.1:9081/auth/v1
CALLBACK=$SUITE/test/a/turna/callback

docker run -d --rm --name turna-oidc-pg -e POSTGRES_PASSWORD=pg -p 55432:5432 postgres:17-alpine >/dev/null
docker compose up -d >/dev/null
trap 'kill $TURNA_PID 2>/dev/null; docker compose down >/dev/null; docker stop turna-oidc-pg >/dev/null' EXIT

until pg_isready -h 127.0.0.1 -p 55432 >/dev/null 2>&1; do sleep 1; done
PGPASSWORD=pg psql -h 127.0.0.1 -p 55432 -U postgres -qc "create database oidc"

go build -o ./turna "$ROOT/cmd/turna"
mkdir -p sessions
CONFIG_FILE=$PWD/turna.yml ./turna > turna.log 2>&1 &
TURNA_PID=$!
until curl -s -o /dev/null "$ADMIN/info"; do sleep 1; done

put() { curl -sf -X PUT "$ADMIN/$1" -H 'Content-Type: application/json' -d "$2" >/dev/null; }
put settings/oauth2 '{"value":{"base_url":"https://turna.local:9443"}}'
put settings/check '{"value":{"no_host_check":true}}'
put settings/authorize '{"value":{"login_url":"/login/"}}'
put settings/token '{"value":{"oidc_standard_claims":true}}'
put oauth/clients/login-ui '{"config":{"client_secret":"login-ui-secret-abcdefgh","scope":["openid"]}}'
for c in 1 2 post; do
  put "oauth/clients/oidc-client-$c" "{\"config\":{\"client_secret\":\"secret-$c-abcdefghijklmnop\",\"scope\":[\"openid\"],\"redirect_uris\":[\"$CALLBACK\"],\"skip_consent\":true}}"
done
curl -sf -X POST "$ADMIN/users" -H 'Content-Type: application/json' -d '{
  "alias":["oidc-user","oidc-user@example.com"],"local":true,"is_active":true,
  "details":{"password":"oidc-pass-123456","name":"OIDC User","given_name":"OIDC","family_name":"User",
    "email":"oidc-user@example.com","email_verified":true,"phone_number":"+15555550100",
    "phone_number_verified":false,"address":{"country":"TR"}}}' >/dev/null

until [ "$(curl -sk -o /dev/null -w '%{http_code}' $SUITE/api/runner/available)" = 200 ]; do sleep 3; done

plan() {
  curl -sk -X POST "$SUITE/api/plan?planName=$1&variant=$2" -H 'Content-Type: application/json' \
    --data @plan-config.json | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])'
}
python3 run.py "$(plan oidcc-config-certification-test-plan '%7B%7D')"
python3 run.py "$(plan oidcc-basic-certification-test-plan '%7B%22server_metadata%22%3A%22discovery%22%2C%22client_registration%22%3A%22static_client%22%7D')"
