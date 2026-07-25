#!/usr/bin/env bash
# Ship a new version of the storefront app through GitOps: bump the manifest in
# Gitea, which ArgoCD syncs → fires a deploy marker to wardn → analysis runs.
#
#   ./deploy-version.sh              # ship a healthy version
#   ./deploy-version.sh --regress    # ship a deliberately slow (regressed) version
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

REGRESS=false
[ "${1:-}" = "--regress" ] && REGRESS=true

need kubectl; need git

kubectl -n gitea port-forward svc/gitea 3000:3000 >/tmp/wardn-gitea-pf.log 2>&1 &
PF=$!; trap 'kill "$PF" 2>/dev/null || true' EXIT
sleep 4

TMP="$(mktemp -d)"
git clone -q "http://wardn:wardn@localhost:3000/wardn/demo.git" "$TMP"
cd "$TMP"

VER="v1.0.$(git rev-list --count HEAD)"   # next commit number → the new version
log "shipping storefront $VER (regressed=$REGRESS)"

awk -v ver="$VER" -v reg="$REGRESS" '
  /name: APP_VERSION/ { print; getline; sub(/value: .*/, "value: \"" ver "\""); print; next }
  /name: REGRESSED/   { print; getline; sub(/value: .*/, "value: \"" reg "\""); print; next }
  { print }
' deployment.yaml > deployment.yaml.tmp && mv deployment.yaml.tmp deployment.yaml

git -c user.email=wardn@example.com -c user.name=wardn -c commit.gpgsign=false commit -aqm "deploy $VER (regressed=$REGRESS)"
git push -q origin main

# Surface the semantic version to the deploy marker: ArgoCD's notification reads
# this annotation (falling back to the short git SHA if it's ever missing), so
# the marker carries "v1.0.N" instead of a commit hash.
kubectl -n argocd annotate app storefront wardn.dev/version="$VER" --overwrite >/dev/null 2>&1 || true
# Nudge ArgoCD to refresh now instead of waiting for its poll interval.
kubectl -n argocd annotate app storefront argocd.argoproj.io/refresh=hard --overwrite >/dev/null 2>&1 || true

log "pushed. ArgoCD will sync the change, then notify wardn (a marker appears on"
log "the Deploys page). After the analysis window elapses, the verdict updates."
