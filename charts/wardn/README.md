# wardn Helm chart

Deploy-aware observability on top of SigNoz. This chart installs the wardn
backend (API + analyzer + alerting), the dashboard, and - by default - a
Postgres for wardn's own history.

> This is the **production** chart. The `deploy/helm/wardn` chart in the repo is
> a demo skeleton used by the kind / e2e scripts and bundles a sample emitter;
> use this one for a real install.

## Prerequisites

- Kubernetes 1.23+
- Helm 3.8+
- A reachable [SigNoz](https://signoz.io/) instance (for the analyzer to query).
  Without it, the marker API and dashboard still work; analysis jobs fail until
  it is set.
- Container images for the backend and frontend. The repo ships Dockerfiles;
  build and push them, then point `backend.image` / `frontend.image` at your
  registry:
  ```bash
  docker build -t <registry>/wardn-backend:0.1.0 .
  docker build -t <registry>/wardn-frontend:0.1.0 ./frontend
  docker push <registry>/wardn-backend:0.1.0
  docker push <registry>/wardn-frontend:0.1.0
  ```

## Install

```bash
helm install wardn ./charts/wardn \
  --namespace wardn --create-namespace \
  --set backend.image.repository=<registry>/wardn-backend \
  --set frontend.image.repository=<registry>/wardn-frontend \
  --set signoz.url=http://signoz.signoz.svc.cluster.local:8080 \
  --set signoz.apiKey=<minted-service-account-key> \
  --set auth.adminPassword=<a-strong-password>
```

Then follow the printed NOTES to reach the dashboard and send your first marker.

## What the chart does for you

- **Generates and persists secrets.** The Postgres password, session secret, and
  the AES key that encrypts UI-stored AI credentials (`WARDN_SECRET_KEY`) are
  generated if you don't supply them, and kept stable across `helm upgrade`.
  Because the encryption key is always set, saving an AI provider key from the UI
  works out of the box.
- **Templates the dashboard's nginx** so `/api` proxies to this release's backend
  Service - you can run more than one release in a namespace.
- **Persists wardn's history** on a PVC by default, so deploy comparisons outlive
  a pod restart. Point it at an external Postgres with `postgres.enabled=false`.

## Common configurations

External Postgres:

```bash
--set postgres.enabled=false \
--set externalDatabase.url='postgres://user:pass@host:5432/wardn?sslmode=require'
```

Expose via Ingress + TLS:

```bash
--set ingress.enabled=true \
--set ingress.className=nginx \
--set ingress.hosts[0].host=wardn.yourco.com \
--set ingress.hosts[0].paths[0].path=/ \
--set ingress.hosts[0].paths[0].pathType=Prefix \
--set ingress.tls[0].secretName=wardn-tls \
--set ingress.tls[0].hosts[0]=wardn.yourco.com \
--set backend.publicBaseUrl=https://wardn.yourco.com
```

Bring your own Secret (no chart-generated credentials). It must contain the keys
`database-url`, `postgres-password`, `session-secret`, `wardn-secret-key`,
`ai-api-key`, `signoz-api-key`, `admin-password`:

```bash
--set existingSecret=my-wardn-secret
```

## Values

| Key | Default | Description |
|---|---|---|
| `backend.image.repository` | `ghcr.io/happymooguild/wardn-backend` | Backend image (override to your registry). |
| `backend.image.tag` | `""` (chart appVersion) | Backend image tag. |
| `backend.replicas` | `1` | Backend replicas. |
| `backend.publicBaseUrl` | `http://localhost:8088` | Base URL used in alert links. Set to your host. |
| `backend.allowLocalWebhooks` | `false` | Permit webhook targets on localhost / private ranges. |
| `frontend.image.repository` | `ghcr.io/happymooguild/wardn-frontend` | Dashboard image. |
| `frontend.service.type` | `ClusterIP` | `ClusterIP` / `NodePort` / `LoadBalancer`. |
| `ingress.enabled` | `false` | Create an Ingress for the dashboard. |
| `postgres.enabled` | `true` | Deploy an in-cluster Postgres. |
| `postgres.persistence.enabled` | `true` | Use a PVC (else ephemeral emptyDir). |
| `postgres.persistence.size` | `8Gi` | PVC size. |
| `externalDatabase.url` | `""` | Connection string when `postgres.enabled=false`. |
| `signoz.url` | `""` | SigNoz query URL (analyzer). |
| `signoz.apiKey` | `""` | SigNoz service-account API key. |
| `signoz.uiUrl` | `""` | Optional SigNoz UI base URL for deep links. |
| `ai.provider` | `""` | `anthropic` / `openai` / `gemini`; inferred from the key. |
| `ai.model` | `""` | Model id (default `claude-opus-4-8` for anthropic). |
| `ai.apiKey` | `""` | Provider key. Prefer `existingSecret` in production. |
| `ai.secretKey` | `""` (generated) | Passphrase encrypting UI-stored AI keys. |
| `auth.adminUser` | `admin` | Initial admin username. |
| `auth.adminPassword` | `admin@12345` | **Change this.** Initial admin password. |
| `auth.sessionSecret` | `""` (generated) | Cookie-signing secret. |
| `seedApps` | `[]` | Pre-register services as `{name,key}` pairs (optional; most add apps in the UI). |
| `existingSecret` | `""` | Use a Secret you manage instead of a generated one. |
| `imagePullSecrets` | `[]` | Pull secrets for private registries. |

See [`values.yaml`](values.yaml) for the full, commented set.

## Uninstall

```bash
helm uninstall wardn -n wardn
# PVCs from the StatefulSet are retained by design; delete them to wipe history:
kubectl delete pvc -n wardn -l app.kubernetes.io/component=postgres
```
