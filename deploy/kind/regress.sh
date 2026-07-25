#!/usr/bin/env bash
# Toggle the sample-app between healthy (v1) and regressed (v2) to demo a bad
# deploy. Watch the latency line on the dashboard react within a few seconds.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
MODE="${1:-on}"

case "$MODE" in
  on|true)   VAL=true ;;
  off|false) VAL=false ;;
  *) echo "usage: regress.sh [on|off]"; exit 1 ;;
esac

echo "==> Setting sampleApp.regressed=$VAL"
helm upgrade wardn "$REPO_ROOT/deploy/helm/wardn" \
  --reuse-values \
  --set sampleApp.regressed="$VAL" \
  --wait

echo "==> Done — watch the dashboard's latency line."
