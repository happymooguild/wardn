#!/usr/bin/env bash
# Delete the whole kind cluster (and everything in it).
set -euo pipefail
CLUSTER="${CLUSTER:-wardn}"
echo "==> Deleting kind cluster '$CLUSTER'"
kind delete cluster --name "$CLUSTER"
