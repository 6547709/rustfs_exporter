#!/usr/bin/env bash
# Uninstall rustfs-exporter + VictoriaMetrics systemd services.
# Run `uninstall.sh --purge-data` to also delete state dirs and user.

set -euo pipefail

[[ $EUID -eq 0 ]] || { echo "must run as root" >&2; exit 1; }

PURGE=0
[[ "${1:-}" == "--purge-data" ]] && PURGE=1

echo ">> stopping and disabling services"
systemctl disable --now rustfs-exporter.service 2>/dev/null || true
systemctl disable --now victoria-metrics.service 2>/dev/null || true

echo ">> removing unit files"
rm -f /etc/systemd/system/rustfs-exporter.service
rm -f /etc/systemd/system/victoria-metrics.service
systemctl daemon-reload

echo ">> removing binaries"
rm -f /usr/local/bin/rustfs-exporter
rm -f /usr/local/bin/victoria-metrics

if [[ $PURGE -eq 1 ]]; then
  echo ">> purging config, state, data"
  rm -rf /etc/rustfs-mon
  rm -rf /var/lib/rustfs-mon
  rm -rf /var/lib/victoria-metrics
  if id rustfs-mon &>/dev/null; then
    userdel rustfs-mon
  fi
  echo ">> purged"
else
  echo ">> keeping /etc/rustfs-mon, /var/lib/rustfs-mon, /var/lib/victoria-metrics, user rustfs-mon"
  echo "   re-run with --purge-data to remove"
fi

echo "Done."