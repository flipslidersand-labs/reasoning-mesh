#!/bin/bash
set -e

echo "=== Endpoint Health Check ==="

echo -n "YUKI Ollama (192.168.68.56:11434)... "
TAGS=$(curl -sf http://192.168.68.56:11434/api/tags)
if [ -n "$TAGS" ]; then
  echo "$TAGS" | python3 -c "import sys,json; models=json.load(sys.stdin).get('models',[]); print('OK —', ', '.join(m['name'] for m in models))"
else
  echo "WARN: no response (Ollama may need to be started on YUKI)"
fi

echo -n "MINIPC e5 embedder (192.168.68.63:9092)... "
curl -sf http://192.168.68.63:9092/health && echo "OK"

echo -n "MINIPC Qdrant (192.168.68.63:6333)... "
curl -sf http://192.168.68.63:6333/healthz && echo "OK"

echo "=== All endpoints OK ==="
