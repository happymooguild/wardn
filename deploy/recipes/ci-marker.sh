#!/usr/bin/env bash
# Post a deploy marker to wardn after a healthy rollout.
#
# Usage:
#   WARDN_URL=http://localhost:8080 \
#   WARDN_API_KEY=wardn_dev_key_checkout \
#   APP=checkout-service \
#   VERSION=v1.0.11 \
#   ./deploy/recipes/ci-marker.sh
set -euo pipefail

WARDN_URL="${WARDN_URL:-http://localhost:8080}"
WARDN_API_KEY="${WARDN_API_KEY:?WARDN_API_KEY is required}"
APP="${APP:-checkout-service}"
VERSION="${VERSION:?VERSION is required}"
ENVIRONMENT="${ENVIRONMENT:-production}"
SOURCE="${SOURCE:-ci}"
TIMESTAMP="${TIMESTAMP:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

curl -fsS -X POST "${WARDN_URL%/}/api/v1/deployments" \
  -H "Authorization: Bearer ${WARDN_API_KEY}" \
  -H "Content-Type: application/json" \
  -d "$(cat <<EOF
{
  "app": "${APP}",
  "version": "${VERSION}",
  "environment": "${ENVIRONMENT}",
  "timestamp": "${TIMESTAMP}",
  "source": "${SOURCE}"
}
EOF
)"
echo
