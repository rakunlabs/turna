#!/usr/bin/env bash
# Protects a demo MCP server with turna and connects to it with the official
# MCP TypeScript SDK client: 401 → RFC 9728 metadata → RFC 8414 discovery →
# RFC 7591 registration → PKCE login and consent → tools. Needs docker, Go,
# Node 20+ and npm.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(git rev-parse --show-toplevel)
ADMIN=http://127.0.0.1:9082/auth/v1

docker run -d --rm --name turna-mcp-pg -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:17-alpine >/dev/null
trap 'kill $TURNA_PID $MCP_PID 2>/dev/null; docker stop turna-mcp-pg >/dev/null' EXIT
until docker exec turna-mcp-pg pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done

npm install --silent
go build -o ./turna "$ROOT/cmd/turna"
mkdir -p sessions

node server.mjs &
MCP_PID=$!
CONFIG_FILE=$PWD/turna.yml ./turna > turna.log 2>&1 &
TURNA_PID=$!
until curl -s -o /dev/null "$ADMIN/info"; do sleep 1; done

put() { curl -sf -X PUT "$ADMIN/$1" -H 'Content-Type: application/json' -d "$2" >/dev/null; }
put settings/registration '{"value":{"enabled":true,"client_lifetime":"720h"}}'
put settings/authorize '{"value":{"login_url":"/login/"}}'
put oauth/clients/ui '{"config":{"client_secret":"ui-secret-change-me","scope":["openid"]}}'
curl -sf -X POST "$ADMIN/users" -H 'Content-Type: application/json' \
  -d '{"alias":["demo"],"local":true,"is_active":true,"details":{"name":"Demo User","password":"demo-password-123"}}' >/dev/null

node client.mjs
