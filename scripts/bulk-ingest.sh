#!/usr/bin/env bash
# Bulk ingest all git commit diffs into reasoning-mesh knowledge base
set -uo pipefail

ORCH_URL="${LLMO_ORCH_URL:-http://localhost:8765}"
TOKEN="${LLMO_TRIGGER_TOKEN:-}"
REPO_DIR="${1:-$(git rev-parse --show-toplevel)}"

# Pass the token via a 0600 header file so it never appears in `ps` output.
AUTH_ARGS=()
if [ -n "$TOKEN" ]; then
  umask 077
  AUTH_HDR_FILE=$(mktemp)
  trap 'rm -f "$AUTH_HDR_FILE"' EXIT
  printf 'Authorization: Bearer %s\n' "$TOKEN" > "$AUTH_HDR_FILE"
  AUTH_ARGS=(-H "@$AUTH_HDR_FILE")
fi

SUCCESS=0
SKIP=0
FAIL=0

echo "Ingesting commits from: $REPO_DIR"
echo "Target: $ORCH_URL/v1/trigger"

while IFS=' ' read -r sha rest; do
  diff=$(git -C "$REPO_DIR" show "$sha" -- '*.go' '*.yaml' '*.yml' '*.md' 2>/dev/null | head -c 16384)
  if [ -z "$diff" ]; then
    SKIP=$((SKIP + 1))
    continue
  fi

  payload=$(jq -n \
    --arg sha "$sha" \
    --arg diff "$diff" \
    '{"commit_sha": $sha, "diff": $diff, "ci_log": ""}')

  status=$(curl -sf -o /dev/null -w "%{http_code}" \
    -X POST "$ORCH_URL/v1/trigger" \
    -H "Content-Type: application/json" \
    ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} \
    -d "$payload" 2>/dev/null || echo "000")

  if [ "$status" = "202" ] || [ "$status" = "200" ]; then
    SUCCESS=$((SUCCESS + 1))
    echo "  OK  $sha"
  else
    FAIL=$((FAIL + 1))
    echo "  FAIL($status) $sha"
  fi

  # avoid hammering ollama
  sleep 0.5
done < <(git -C "$REPO_DIR" log --oneline)

echo ""
echo "Done: success=$SUCCESS skip=$SKIP fail=$FAIL"
