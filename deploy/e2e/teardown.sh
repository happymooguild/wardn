#!/usr/bin/env bash
# Delete the whole e2e kind cluster (and everything in it).
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
# Kill any port-forwards this repo's scripts may have left running.
pkill -f "port-forward svc/gitea 3000:3000" 2>/dev/null || true
log "deleting kind cluster '$CLUSTER'"
kind delete cluster --name "$CLUSTER"
