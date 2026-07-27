# Installing wardn

wardn ships two ways: a **Docker Compose** stack for trying it locally, and a
**Helm chart** (published as an OCI artifact on GHCR) for Kubernetes. Both use
the same public images:

- `ghcr.io/happymooguild/wardn-backend`
- `ghcr.io/happymooguild/wardn-frontend`
- chart: `oci://ghcr.io/happymooguild/charts/wardn` (also listed on
  [Artifact Hub](https://artifacthub.io/packages/helm/wardn/wardn))

- [Prerequisites](#prerequisites)
- [Option A - Docker Compose](#option-a---docker-compose-local)
- [Option B - Kubernetes with Helm](#option-b---kubernetes-with-helm)
  - [Verify the chart](#1-verify-the-chart-no-login-needed)
  - [Install](#2-install)
  - [Reach the dashboard](#3-reach-the-dashboard)
  - [Expose with Ingress + TLS](#4-expose-with-ingress--tls)
  - [External Postgres](#external-postgres)
  - [Bring your own Secret](#bring-your-own-secret)
- [Connect SigNoz](#connect-signoz)
- [First deploy marker](#first-deploy-marker)
- [Upgrade](#upgrade)
- [Uninstall](#uninstall)
- [Troubleshooting](#troubleshooting)

---

## Prerequisites

- **Kubernetes 1.23+** and **Helm 3.8+** (for the OCI chart), or **Docker +
  Docker Compose** for the local stack.
- A reachable **[SigNoz](https://signoz.io/)** instance for the analyzer to
  query. Optional to start - the marker API and dashboard work without it, but
  analysis jobs fail until it's configured.
- Optionally an **AI provider key** (Anthropic, OpenAI, or Gemini) for the Ask
  AI root-cause feature. Can also be set later from the UI.

No cluster credentials are needed by wardn itself - it only receives deploy
markers and reads from SigNoz.

---

## Option A - Docker Compose (local)

```bash
git clone https://github.com/happymooguild/wardn.git
cd wardn

# Optional - point the analyzer at a SigNoz instance:
export SIGNOZ_URL=https://your-signoz
export SIGNOZ_API_KEY=...

docker compose up --build
```

Open <http://localhost:8088> and log in as **`admin` / `admin@12345`**.

Fire a deploy marker (analysis runs after the after-window):

```bash
./demo/deploy-marker.sh v1.0.11
```

---

## Option B - Kubernetes with Helm

### 1. Verify the chart (no login needed)

```bash
helm show chart oci://ghcr.io/happymooguild/charts/wardn --version 1.0.0
```

If that prints the chart metadata, the public OCI path is working.

### 2. Install

```bash
helm install wardn oci://ghcr.io/happymooguild/charts/wardn \
  --version 1.0.0 \
  --namespace wardn --create-namespace \
  --set signoz.url=http://signoz.signoz.svc.cluster.local:8080 \
  --set signoz.apiKey='<minted-service-account-key>' \
  --set auth.adminPassword='<a-strong-password>'
```

What the chart sets up for you:

- **Postgres** with a PVC (data survives restarts). Point it at an external
  database instead - see [External Postgres](#external-postgres).
- **Generated secrets** - the Postgres password, the session secret, and the
  AES key that encrypts UI-stored AI credentials (`WARDN_SECRET_KEY`) are
  generated if you don't supply them and kept stable across `helm upgrade`.
  Because the encryption key is always present, saving an AI key from the UI
  works out of the box.
- **Templated dashboard proxy** - nginx's `/api` route points at this release's
  backend Service, so you can run more than one release per namespace.

Watch it come up:

```bash
kubectl -n wardn get pods -w
# wait for wardn-backend, wardn-frontend, wardn-postgres to be Ready
```

### 3. Reach the dashboard

The default Service type is `ClusterIP`. Port-forward to try it:

```bash
kubectl -n wardn port-forward svc/wardn-frontend 8088:80
# open http://localhost:8088  → log in as  admin / <the password you set>
```

### 4. Expose with Ingress + TLS

```bash
helm upgrade wardn oci://ghcr.io/happymooguild/charts/wardn --version 1.0.0 \
  --namespace wardn --reuse-values \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set 'ingress.hosts[0].host=wardn.yourco.com' \
  --set 'ingress.hosts[0].paths[0].path=/' \
  --set 'ingress.hosts[0].paths[0].pathType=Prefix' \
  --set 'ingress.tls[0].secretName=wardn-tls' \
  --set 'ingress.tls[0].hosts[0]=wardn.yourco.com' \
  --set backend.publicBaseUrl=https://wardn.yourco.com
```

`backend.publicBaseUrl` is the base URL wardn puts into alert links, so set it
to your real host.

### External Postgres

Skip the in-cluster database and use your own:

```bash
--set postgres.enabled=false \
--set externalDatabase.url='postgres://user:pass@host:5432/wardn?sslmode=require'
```

### Bring your own Secret

To manage all credentials yourself, create a Secret with these keys and pass its
name. When set, the chart creates no Secret of its own:

```
database-url  postgres-password  session-secret  wardn-secret-key
ai-api-key    signoz-api-key     admin-password
```

```bash
--set existingSecret=my-wardn-secret
```

The full, commented value set lives in
[`charts/wardn/values.yaml`](../charts/wardn/values.yaml); a values reference is
in the [chart README](../charts/wardn/README.md).

---

## Connect SigNoz

wardn needs a SigNoz query URL and a service-account API key.

- `signoz.url` - e.g. `http://signoz.signoz.svc.cluster.local:8080`
- `signoz.apiKey` - a minted service-account key sent as the `SIGNOZ-API-KEY`
  header.

On a fresh self-hosted SigNoz two things trip people up: it ingests nothing
until an organization exists (`POST /api/v1/register`), and there is no static
API key - you mint one for a service account through the session API and assign
it a role (a missing role assignment shows up as a `403`, not a `401`). The
repo's bring-up script under `deploy/e2e/` automates the whole flow if you want
a reference.

For the AI-assisted version comparison to filter cleanly, emit your app's
version as a **datapoint attribute** named `version` (a resource attribute is
not filterable in SigNoz PromQL).

---

## First deploy marker

Register an app in the dashboard (**Deploys → Add app**) to mint a per-app key,
then send a marker the moment a rollout is confirmed healthy.

Direct CI:

```bash
curl -fsS -X POST "$WARDN_URL/api/v1/deployments" \
  -H "Authorization: Bearer $WARDN_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"app":"checkout","version":"v1.4.2","environment":"production","source":"ci"}'
```

GitOps (ArgoCD Notifications) - fire on `Succeeded + Healthy`, `oncePer` sync
revision; see [`deploy/recipes/argocd-notifications-cm.yaml`](../deploy/recipes/argocd-notifications-cm.yaml).
From the second deploy onward the before/after graphs appear, regressions get
flagged, and Ask AI can explain any of them.

---

## Upgrade

```bash
# bump --version to the release you want; --reuse-values keeps your settings
helm upgrade wardn oci://ghcr.io/happymooguild/charts/wardn \
  --version <new-version> --namespace wardn --reuse-values
```

Generated secrets are preserved across upgrades.

## Uninstall

```bash
helm uninstall wardn -n wardn

# PVCs are retained by design; delete them to wipe wardn's history:
kubectl delete pvc -n wardn -l app.kubernetes.io/component=postgres
```

---

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `helm show chart ...` gives a 401/403 | The GHCR packages are still private. Make `wardn-backend`, `wardn-frontend`, and `charts/wardn` public in GitHub → Packages. |
| Pods stuck `ImagePullBackOff` | Same as above, or a private registry - set `imagePullSecrets`. |
| Analysis jobs fail: "metrics provider not configured" | `signoz.url` / `signoz.apiKey` unset or wrong. The rest of the UI still works. |
| SigNoz returns `403` when querying | The service-account key has no role assigned - assign one (separate API call from key creation). |
| "AI keys cannot be saved" banner | `WARDN_SECRET_KEY` is unset. The chart generates one automatically; if you use `existingSecret`, include a `wardn-secret-key`. |
| A healthy version reads a stale, higher latency | PromQL carried the previous version's last sample forward. Emit `version` as a datapoint attribute so wardn can filter to one exact version. |
| Postgres won't start on a restricted cluster | The stock Postgres image manages its own user; if a Pod Security admission blocks it, use `postgres.enabled=false` and an external database. |

More context on the SigNoz integration and the AI layer is in
[`docs/design-doc.md`](design-doc.md) and [`docs/ai-layer-design.md`](ai-layer-design.md).
