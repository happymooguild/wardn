#!/usr/bin/env bash
# One-command local bring-up: kind cluster -> build images -> load -> helm install.
set -euo pipefail

CLUSTER="${CLUSTER:-wardn}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

BACKEND_IMG="wardn-backend:dev"
FRONTEND_IMG="wardn-frontend:dev"
SAMPLE_IMG="wardn-sample-app:dev"

echo "==> Ensuring kind cluster '$CLUSTER'"
if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  echo "    cluster already exists"
else
  kind create cluster --config "$SCRIPT_DIR/kind-cluster.yaml"
fi

echo "==> Building images"
docker build -t "$BACKEND_IMG"  "$REPO_ROOT"
docker build -t "$FRONTEND_IMG" "$REPO_ROOT/frontend"
docker build -t "$SAMPLE_IMG"   "$REPO_ROOT/demo/sample-app"

echo "==> Loading images into kind"
kind load docker-image --name "$CLUSTER" "$BACKEND_IMG" "$FRONTEND_IMG" "$SAMPLE_IMG"

echo "==> Deploying with Helm"
helm upgrade --install wardn "$REPO_ROOT/deploy/helm/wardn" --wait --timeout 180s

echo
echo "==> Done."
echo "    Dashboard:        http://localhost:8088"
echo "    Pods:             kubectl get pods"
echo "    Simulate a bad deploy:  $SCRIPT_DIR/regress.sh on"
echo "    Back to healthy:        $SCRIPT_DIR/regress.sh off"
echo "    Tear it all down:       $SCRIPT_DIR/teardown.sh"
