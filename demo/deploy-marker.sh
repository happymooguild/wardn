#!/usr/bin/env bash
# Fire a deploy marker against a local wardn for analyzer testing.
#
# Example:
#   ./demo/deploy-marker.sh v1.0.11
#   REGRESSED path: flip sample-app REGRESSED=true, wait for metrics, then marker.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${1:-v1.0.11}"
export WARDN_URL="${WARDN_URL:-http://localhost:8080}"
export WARDN_API_KEY="${WARDN_API_KEY:-wardn_dev_key_checkout}"
export APP="${APP:-checkout-service}"
export VERSION
export SOURCE="${SOURCE:-manual}"

exec "$ROOT/deploy/recipes/ci-marker.sh"
