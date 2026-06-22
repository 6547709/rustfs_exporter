#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

for tar in images/*.tar; do
  echo "Loading $tar ..."
  docker load -i "$tar"
done
echo "Done. Images now available:"
docker images | grep -E 'rustfs|victoria|grafana' || true