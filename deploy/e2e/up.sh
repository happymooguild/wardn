#!/usr/bin/env bash
# End-to-end bring-up: kind + wardn + SigNoz + ArgoCD/Gitea + observed demo app.
#
#   ./up.sh                 # everything
#   ./up.sh --no-argocd     # skip ArgoCD + Gitea (kind + wardn + SigNoz)
#   ./up.sh --no-signoz     # skip SigNoz (analyzer will no-op; marker/alert UI still work)
#   ./up.sh --no-build      # reuse already-loaded images
#
# Heads-up: the full stack wants ~6-8 GB of free RAM (SigNoz runs ClickHouse).
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

WITH_SIGNOZ=1; WITH_ARGOCD=1; DO_BUILD=1
for a in "$@"; do case "$a" in
  --no-signoz) WITH_SIGNOZ=0 ;;
  --no-argocd) WITH_ARGOCD=0 ;;
  --no-build)  DO_BUILD=0 ;;
  *) die "unknown flag: $a" ;;
esac; done

# ---- preflight ----
for t in docker kind kubectl helm git curl; do need "$t"; done

# ---- cluster ----
if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  log "kind cluster '$CLUSTER' already exists"
else
  log "creating kind cluster '$CLUSTER'"
  kind create cluster --config "$E2E_DIR/kind-cluster.yaml"
fi
kubectl config use-context "kind-$CLUSTER" >/dev/null

# ---- images ----
if [ "$DO_BUILD" = 1 ]; then
  log "building images"
  docker build -t wardn-backend:dev   "$REPO_ROOT"
  docker build -t wardn-frontend:dev  "$REPO_ROOT/frontend"
  docker build -t wardn-sample-app:dev "$REPO_ROOT/demo/sample-app"
fi
log "loading images into kind"
kind load docker-image --name "$CLUSTER" wardn-backend:dev wardn-frontend:dev wardn-sample-app:dev

# ---- SigNoz ----
SIGNOZ_URL=""; OTLP_ENDPOINT=""; SIGNOZ_API_KEY=""
if [ "$WITH_SIGNOZ" = 1 ]; then
  log "installing SigNoz (this is the slow part - ClickHouse + collector)"
  helm repo add signoz https://charts.signoz.io >/dev/null 2>&1 || true
  helm repo update signoz >/dev/null 2>&1 || true
  kubectl create namespace signoz --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  helm upgrade --install signoz signoz/signoz -n signoz --timeout 15m

  log "waiting for the SigNoz OTLP collector service to appear"
  for _ in $(seq 1 60); do
    OTLP_SVC="$(find_svc signoz 'otel-collector|otel-agent|collector')"
    [ -n "$OTLP_SVC" ] && break
    sleep 5
  done
  QUERY_SVC="$(find_svc signoz 'query|^signoz$')"
  [ -n "${OTLP_SVC:-}" ]  || { warn "couldn't find the SigNoz collector svc - defaulting"; OTLP_SVC="signoz-otel-collector"; }
  [ -n "${QUERY_SVC:-}" ] || { warn "couldn't find the SigNoz query svc - defaulting";     QUERY_SVC="signoz-query-service"; }
  OTLP_ENDPOINT="http://${OTLP_SVC}.signoz.svc.cluster.local:4318"
  SIGNOZ_URL="http://${QUERY_SVC}.signoz.svc.cluster.local:8080"
  log "SigNoz OTLP=$OTLP_ENDPOINT  query=$SIGNOZ_URL  (verify with: kubectl -n signoz get svc)"

  # CRITICAL: SigNoz will not register its otel-collector - so the OTLP receiver
  # never binds and no metrics are ingested - until a first org/admin exists.
  # Create one via its API (retries until the query service is ready).
  log "creating SigNoz admin/org (required before it accepts metrics)"
  kubectl -n signoz port-forward "svc/${QUERY_SVC}" 18080:8080 >/tmp/wardn-sz-pf.log 2>&1 &
  SZPF=$!; sleep 5
  SZ_DONE=0
  for _ in $(seq 1 60); do
    if curl -s http://localhost:18080/api/v1/version 2>/dev/null | grep -q '"setupCompleted":true'; then
      log "SigNoz already set up"; SZ_DONE=1; break
    fi
    if curl -fsS -X POST http://localhost:18080/api/v1/register -H 'Content-Type: application/json' \
        -d '{"email":"admin@wardn.local","password":"Password@123","name":"admin","orgName":"wardn"}' >/dev/null 2>&1; then
      log "SigNoz admin created (admin@wardn.local / Password@123)"; SZ_DONE=1; break
    fi
    sleep 10
  done
  [ "$SZ_DONE" = 1 ] || warn "SigNoz registration didn't complete - sign up at the SigNoz UI so it accepts metrics"

  # The analyzer queries SigNoz's API, which requires an API key (SIGNOZ-API-KEY
  # header). Self-hosted SigNoz (v0.133) has no static key: mint one via a
  # service account. Flow: session context → orgId, login → JWT, ensure a
  # 'wardn-analyzer' service account with the signoz-admin role, then create a
  # non-expiring key. Re-runnable (reuses the service account if it exists).
  if [ "$SZ_DONE" = 1 ]; then
    log "minting a SigNoz API key for the analyzer (service account 'wardn-analyzer')"
    B=http://localhost:18080
    SZ_EMAIL=admin@wardn.local; SZ_PASS='Password@123'
    ORG_ID="$(curl -s "$B/api/v2/sessions/context?email=${SZ_EMAIL}&ref=http://localhost" \
      | python3 -c "import sys,json;o=json.load(sys.stdin).get('data',{}).get('orgs',[]);print(o[0]['id'] if o else '')" 2>/dev/null || true)"
    JWT="$(curl -s -X POST "$B/api/v2/sessions/email_password" -H 'Content-Type: application/json' \
      -d "{\"email\":\"${SZ_EMAIL}\",\"password\":\"${SZ_PASS}\",\"orgID\":\"${ORG_ID}\"}" \
      | python3 -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('accessToken',''))" 2>/dev/null || true)"
    if [ -n "$JWT" ]; then
      ADMIN_ROLE="$(curl -s -H "Authorization: Bearer $JWT" "$B/api/v1/roles" \
        | python3 -c "import sys,json;print(next((r['id'] for r in json.load(sys.stdin).get('data',[]) if r.get('name')=='signoz-admin'),''))" 2>/dev/null || true)"
      SAID="$(curl -s -H "Authorization: Bearer $JWT" "$B/api/v1/service_accounts" \
        | python3 -c "import sys,json;print(next((s['id'] for s in (json.load(sys.stdin).get('data') or []) if s.get('name')=='wardn-analyzer'),''))" 2>/dev/null || true)"
      if [ -z "$SAID" ]; then
        SAID="$(curl -s -X POST "$B/api/v1/service_accounts" -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json' \
          -d '{"name":"wardn-analyzer"}' \
          | python3 -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)"
      fi
      if [ -n "$SAID" ] && [ -n "$ADMIN_ROLE" ]; then
        curl -s -o /dev/null -X POST "$B/api/v1/service_accounts/$SAID/roles" \
          -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json' -d "{\"id\":\"$ADMIN_ROLE\"}" || true
        # A key's secret is returned only at creation and re-creating a name 409s,
        # so use a unique name each run - the analyzer just needs one working key.
        SIGNOZ_API_KEY="$(curl -s -X POST "$B/api/v1/service_accounts/$SAID/keys" \
          -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json' \
          -d "{\"name\":\"wardn-analyzer-key-$(date +%s)\",\"expiresAt\":0}" \
          | python3 -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('key',''))" 2>/dev/null || true)"
      fi
    fi
    [ -n "$SIGNOZ_API_KEY" ] && log "SigNoz analyzer key minted" \
      || warn "couldn't mint a SigNoz API key - the analyzer will report 'metrics provider not configured'"
  fi
  kill "$SZPF" 2>/dev/null || true
fi

# ---- wardn ----
# Random key so the UI can store AI provider credentials encrypted at rest.
# Reuse the existing one on re-runs so already-stored keys stay decryptable.
WARDN_SECRET_KEY="$(kubectl get secret wardn-secrets -o jsonpath='{.data.wardn-secret-key}' 2>/dev/null | base64 -d 2>/dev/null || true)"
if [ -z "$WARDN_SECRET_KEY" ]; then
  WARDN_SECRET_KEY="$(openssl rand -hex 32 2>/dev/null || head -c32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
fi
log "installing wardn"
helm upgrade --install wardn "$REPO_ROOT/deploy/helm/wardn" \
  -f "$E2E_DIR/values-e2e.yaml" \
  --set backend.signozUrl="$SIGNOZ_URL" \
  --set backend.signozApiKey="$SIGNOZ_API_KEY" \
  --set backend.secretKey="$WARDN_SECRET_KEY" \
  --set sampleApp.otlpEndpoint="$OTLP_ENDPOINT" \
  --wait --timeout 5m
log "wardn is up - dashboard http://localhost:8088  (admin / admin@12345)"

# ---- Gitea + ArgoCD ----
if [ "$WITH_ARGOCD" = 1 ]; then
  bash "$E2E_DIR/gitops.sh"
fi

# ---- done ----
cat <<EOF

$(printf '\033[1;32m====================  wardn e2e is up  ====================\033[0m')

  Dashboard : http://localhost:8088     (admin / admin@12345)

  SigNoz UI : kubectl -n signoz port-forward svc/${QUERY_SVC:-signoz} 3301:3301   → http://localhost:3301
  ArgoCD UI : kubectl -n argocd port-forward svc/argocd-server 8092:443          → https://localhost:8092
              user: admin   pass: kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d
  Gitea     : kubectl -n gitea  port-forward svc/gitea 3000:3000                 → http://localhost:3000 (wardn/wardn)

  Trigger a deploy (→ ArgoCD sync → marker → analysis):
    ./deploy-version.sh --regress     # ship a bad version
    ./deploy-version.sh               # ship a healthy version

  Tear it all down:
    ./teardown.sh
EOF
