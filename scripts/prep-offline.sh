#!/usr/bin/env bash
# 把监控栈的镜像（exporter + vm + grafana）打成 tar 到 images/，用于离线部署。
# rustfs 镜像默认不在此栈中（被监控的 rustfs 是外部依赖）；若需自包含 rustfs，
# 可设 PREP_RUSTFS=1 让脚本也拉 rustfs 镜像。
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p images

declare -A IMAGES=(
  [exporter]="local-mirror/rustfs-exporter:latest"
  [vm]="victoriametrics/victoria-metrics:v1.146.0"
  [grafana]="grafana/grafana:13.0.2"
)

if [[ "${PREP_RUSTFS:-0}" == "1" ]]; then
  IMAGES[rustfs]="${RUSTFS_IMAGE:-local-mirror/rustfs:latest}"
fi

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