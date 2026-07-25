#!/usr/bin/env bash
# Gitea (seeded demo repo) + ArgoCD + the storefront Application.
# Split out of up.sh so it can be re-run on its own once the cluster, SigNoz, and
# wardn are already up.
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
need kubectl; need git; need curl

# Discover the SigNoz OTLP collector endpoint (empty if SigNoz isn't installed).
OTLP_SVC="$(find_svc signoz 'otel-collector|otel-agent|collector')"
OTLP_ENDPOINT=""; [ -n "$OTLP_SVC" ] && OTLP_ENDPOINT="http://${OTLP_SVC}.signoz.svc.cluster.local:4318"

log "installing Gitea"
kubectl apply -f "$E2E_DIR/gitea/gitea.yaml" >/dev/null
wait_rollout gitea gitea

log "creating Gitea admin + demo repo"
# Gitea's CLI refuses to run as root (kubectl exec's default user), so drop to git.
kubectl -n gitea exec deploy/gitea -- su git -c \
  "gitea admin user create --username wardn --password wardn --email wardn@example.com --admin --must-change-password=false" 2>&1 \
  | grep -iv 'already exists' || true

kubectl -n gitea port-forward svc/gitea 3000:3000 >/tmp/wardn-gitea-pf.log 2>&1 &
PF=$!; trap 'kill "$PF" 2>/dev/null || true' EXIT
sleep 4
curl -fsS -u wardn:wardn -X POST http://localhost:3000/api/v1/user/repos \
  -H 'Content-Type: application/json' \
  -d '{"name":"demo","private":false,"auto_init":true,"default_branch":"main"}' >/dev/null 2>&1 || true

TMP="$(mktemp -d)"
git clone -q "http://wardn:wardn@localhost:3000/wardn/demo.git" "$TMP"
sed "s|__OTLP_ENDPOINT__|${OTLP_ENDPOINT}|g" "$E2E_DIR/demo-app/deployment.yaml" > "$TMP/deployment.yaml"
(
  cd "$TMP"
  git add -A
  git -c user.email=wardn@example.com -c user.name=wardn commit -q -m "seed storefront" 2>/dev/null || true
  git push -q origin main 2>/dev/null || true
)
rm -rf "$TMP"; kill "$PF" 2>/dev/null || true; trap - EXIT
log "demo repo seeded"

log "installing ArgoCD"
kubectl create namespace argocd --dry-run=client -o yaml | kubectl apply -f - >/dev/null
# Server-side apply: the ArgoCD CRDs are too large for client-side apply's
# last-applied-configuration annotation (256KB limit).
kubectl apply --server-side --force-conflicts -n argocd \
  -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml >/dev/null
kubectl apply -f "$E2E_DIR/argocd/notifications.yaml" >/dev/null
wait_rollout argocd argocd-server 420s
wait_rollout argocd argocd-notifications-controller 300s || warn "notifications controller not ready yet"
kubectl -n argocd rollout restart deploy/argocd-notifications-controller >/dev/null 2>&1 || true
kubectl apply -f "$E2E_DIR/argocd/application.yaml" >/dev/null
log "ArgoCD Application 'storefront' created — it will sync the demo app from Gitea"
