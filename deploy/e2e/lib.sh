# Shared helpers for the e2e scripts. Sourced, not executed.

CLUSTER="${CLUSTER:-wardn-e2e}"
E2E_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$E2E_DIR/../.." && pwd)"

# In-cluster addresses (static; kind DNS).
WARDN_BACKEND_URL="http://wardn-backend.default.svc.cluster.local:8080"
GITEA_INCLUSTER="http://gitea.gitea.svc.cluster.local:3000"
DEMO_REPO_PATH="wardn/demo"       # <owner>/<repo> in gitea
STOREFRONT_KEY="wardn_dev_key_storefront"

log()  { printf '\n\033[1;36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!  %s\033[0m\n' "$*"; }
die()  { printf '\033[1;31mERROR: %s\033[0m\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"; }

# Wait for a rollout to become Available.
wait_rollout() { # ns deploy [timeout]
  local ns="$1" dep="$2" to="${3:-300s}"
  kubectl -n "$ns" rollout status "deploy/$dep" --timeout="$to"
}

# Wait until at least one Service in a namespace matches a name substring;
# echoes the first matching service name.
find_svc() { # ns substring
  local ns="$1" pat="$2" name
  name="$(kubectl -n "$ns" get svc -o name 2>/dev/null | sed 's|service/||' | grep -iE "$pat" | head -1 || true)"
  printf '%s' "$name"
}
