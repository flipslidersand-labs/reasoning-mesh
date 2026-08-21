#!/bin/bash
set -euo pipefail

OLLAMA_URL="${OLLAMA_URL:-http://192.168.68.56:11434}"
EMBEDDER_URL="${EMBEDDER_URL:-http://192.168.68.63:9092}"
QDRANT_URL="${QDRANT_URL:-http://192.168.68.63:6333}"

ok()   { echo "OK — $*"; }
fail() { echo "FAIL — $*" >&2; exit 1; }

echo "=== Endpoint Health Check ==="

# YUKI Ollama
printf "YUKI Ollama (%s)... " "$OLLAMA_URL"
TAGS=$(curl -sf --max-time 10 "$OLLAMA_URL/api/tags") || fail "no response"
python3 -c "
import sys, json
models = json.loads(sys.argv[1]).get('models', [])
names = ', '.join(m['name'] for m in models)
print('OK —', names if names else '(no models)')
" "$TAGS"

# MINIPC e5 embedder
printf "MINIPC embedder (%s)... " "$EMBEDDER_URL"
STATUS=$(curl -sf --max-time 10 "$EMBEDDER_URL/health") || fail "no response"
python3 -c "
import sys, json
d = json.loads(sys.argv[1])
s = d.get('status', 'unknown')
if s != 'ok': raise SystemExit(f'status={s}')
print('OK')
" "$STATUS"

# MINIPC Qdrant
printf "MINIPC Qdrant (%s)... " "$QDRANT_URL"
curl -sf --max-time 10 "$QDRANT_URL/healthz" > /dev/null || fail "no response"
echo "OK"

echo "=== All endpoints OK ==="
