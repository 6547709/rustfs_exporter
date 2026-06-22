#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p images

declare -A IMAGES=(
  [rustfs]="${RUSTFS_IMAGE:-local-mirror/rustfs:latest}"
  [exporter]="local-mirror/rustfs-exporter:latest"
  [vm]="victoriametrics/victoria-metrics:latest"
  [grafana]="grafana/grafana:latest"
)

# exporter 需要先构建（如未构建）
if ! docker image inspect "${IMAGES[exporter]}" >/dev/null 2>&1; then
  echo ">> exporter image not found locally; building"
  docker build -t "${IMAGES[exporter]}" exporter
fi

for name in "${!IMAGES[@]}"; do
  img="${IMAGES[$name]}"
  echo ">> $img"
  docker pull "$img" || true
  docker save -o "images/$name.tar" "$img"
done

echo "Done. Tar files:"
ls -lh images/