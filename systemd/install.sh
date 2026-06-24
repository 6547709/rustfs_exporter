#!/usr/bin/env bash
# Idempotent installer for rustfs-exporter + VictoriaMetrics systemd services.
# Tested on RHEL 9 / Rocky 9. SELinux enforcing/permissive/disabled all OK.
#
# Usage:
#   sudo ./install.sh                      # install with default paths
#   EXPORTER_BIN=/path/to/rustfs-exporter sudo ./install.sh
#
# Prerequisites:
#   - rustfs-exporter static binary available locally (build via
#     `cd exporter && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../rustfs-exporter ./cmd/exporter`)
#   - victoria-metrics tarball or binary already downloaded
#
# Run `uninstall.sh --purge-data` to reverse.

set -euo pipefail

THIS_DIR="$(cd "$(dirname "$0")" && pwd)"
MON_DIR="$(cd "$THIS_DIR/.." && pwd)"

# -------- Configurable inputs --------
EXPORTER_BIN="${EXPORTER_BIN:-$MON_DIR/exporter/rustfs-exporter}"
VM_TARBALL="${VM_TARBALL:-}"           # path to vm tarball; if empty, skip VM install
EXPORTER_ENV="${EXPORTER_ENV:-}"       # path to filled-in env file; if empty, copy template
VM_ENV="${VM_ENV:-}"

# -------- Layout --------
BIN_DIR=/usr/local/bin
ETC_DIR=/etc/rustfs-mon
STATE_DIR=/var/lib/rustfs-mon
DATA_DIR=/var/lib/victoria-metrics
USER_NAME=rustfs-mon

# -------- Preflight --------
[[ $EUID -eq 0 ]] || { echo "must run as root (use sudo)" >&2; exit 1; }

command -v systemctl >/dev/null || { echo "systemctl not found — this installer requires systemd" >&2; exit 1; }
command -v restorecon  >/dev/null || echo "note: restorecon not present (SELinux tools not installed)"

# -------- 1. System user --------
if ! id "$USER_NAME" &>/dev/null; then
  echo ">> creating user $USER_NAME"
  useradd --system --shell /sbin/nologin --home-dir "$STATE_DIR" "$USER_NAME"
fi

# -------- 2. Directories --------
echo ">> creating directories"
install -d -o root     -g root     -m 0755 "$BIN_DIR"
install -d -o root     -g root     -m 0750 "$ETC_DIR"
install -d -o root     -g root     -m 0750 "$ETC_DIR/victoria-metrics"
install -d -o "$USER_NAME" -g "$USER_NAME" -m 0750 "$STATE_DIR"
install -d -o "$USER_NAME" -g "$USER_NAME" -m 0750 "$DATA_DIR"

# -------- 3. SELinux labels --------
# /usr/local/bin gets bin_t automatically; /etc gets etc_t; /var/lib gets var_lib_t.
# restorecon is a belt-and-suspenders measure in case the policy was customized.
if command -v restorecon &>/dev/null; then
  echo ">> restoring SELinux labels"
  restorecon -R "$BIN_DIR" "$ETC_DIR" "$STATE_DIR" "$DATA_DIR" 2>/dev/null || true
fi

# -------- 4. Exporter binary --------
if [[ -f "$EXPORTER_BIN" ]]; then
  echo ">> installing exporter to $BIN_DIR/rustfs-exporter"
  install -m 0755 "$EXPORTER_BIN" "$BIN_DIR/rustfs-exporter"
else
  echo "!! exporter binary not found at $EXPORTER_BIN"
  echo "   build: cd $MON_DIR/exporter && CGO_ENABLED=0 go build -trimpath -ldflags=\"-s -w\" -o $BIN_DIR/rustfs-exporter ./cmd/exporter"
  echo "   skipping exporter binary install (unit will fail to start until you provide the binary)"
fi

# -------- 5. VictoriaMetrics --------
if [[ -n "$VM_TARBALL" && -f "$VM_TARBALL" ]]; then
  echo ">> installing VictoriaMetrics from $VM_TARBALL"
  tmp=$(mktemp -d)
  tar -xzf "$VM_TARBALL" -C "$tmp"
  VM_BIN=$(find "$tmp" -maxdepth 1 -name 'victoria-metrics-prod' -print -quit)
  if [[ -z "$VM_BIN" ]]; then
    echo "!! victoria-metrics-prod not found in tarball" >&2
    rm -rf "$tmp"
    exit 1
  fi
  install -m 0755 "$VM_BIN" "$BIN_DIR/victoria-metrics"
  rm -rf "$tmp"
elif [[ -x "$BIN_DIR/victoria-metrics" ]]; then
  echo ">> VictoriaMetrics already at $BIN_DIR/victoria-metrics, skipping"
else
  echo "!! VM_TARBALL not set and $BIN_DIR/victoria-metrics missing — VM service will fail to start"
  echo "   download from https://github.com/VictoriaMetrics/VictoriaMetrics/releases (e.g. v1.146.0)"
fi

# -------- 6. Unit files --------
echo ">> installing systemd units"
install -m 0644 "$THIS_DIR/etc/rustfs-exporter.service"  /etc/systemd/system/rustfs-exporter.service
install -m 0644 "$THIS_DIR/etc/victoria-metrics.service" /etc/systemd/system/victoria-metrics.service

# -------- 7. Env files (only if not present — never overwrite user data) --------
if [[ ! -f "$ETC_DIR/exporter.env" ]]; then
  src="${EXPORTER_ENV:-$THIS_DIR/env/rustfs-exporter.env.example}"
  echo ">> seeding $ETC_DIR/exporter.env from $src"
  install -m 0640 -o root -g "$USER_NAME" "$src" "$ETC_DIR/exporter.env"
  echo "   *** edit $ETC_DIR/exporter.env before enabling the service ***"
fi
if [[ ! -f "$ETC_DIR/victoria-metrics.env" ]]; then
  src="${VM_ENV:-$THIS_DIR/env/victoria-metrics.env.example}"
  echo ">> seeding $ETC_DIR/victoria-metrics.env from $src"
  install -m 0640 -o root -g "$USER_NAME" "$src" "$ETC_DIR/victoria-metrics.env"
fi

# -------- 8. Scrape config (only if not present) --------
if [[ ! -f "$ETC_DIR/victoria-metrics/scrape.yml" ]]; then
  echo ">> seeding scrape config"
  install -m 0640 -o root -g "$USER_NAME" \
    "$THIS_DIR/etc/victoria-metrics/scrape.yml.example" \
    "$ETC_DIR/victoria-metrics/scrape.yml"
fi

# -------- 9. Enable --------
echo ">> reloading systemd"
systemctl daemon-reload
echo ">> enabling services (use --no-start to skip)"
if [[ "${NO_START:-0}" != "1" ]]; then
  systemctl enable --now rustfs-exporter.service victoria-metrics.service
else
  systemctl enable rustfs-exporter.service victoria-metrics.service
fi

echo ""
echo "Done. Useful commands:"
echo "  systemctl status rustfs-exporter victoria-metrics"
echo "  journalctl -u rustfs-exporter -f"
echo "  journalctl -u victoria-metrics -f"
echo "  curl -s localhost:9090/metrics | grep ^rustfs_"
echo "  curl -s 'localhost:8429/api/v1/query?query=rustfs_up'"