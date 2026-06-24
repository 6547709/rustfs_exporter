#!/usr/bin/env bash
# SELinux three-mode matrix test for rustfs-exporter + victoria-metrics services.
# Run on a Rocky 9 / RHEL 9 host. Switches enforcing/permissive/disabled
# and verifies services stay healthy with no AVC denials.
#
# Usage:
#   sudo ./selinux.sh enforcing    # switch to enforcing, test, restore
#   sudo ./selinux.sh permissive   # switch to permissive, test
#   sudo ./selinux.sh disabled     # requires reboot; we just check post-boot state
#   sudo ./selinux.sh all          # run all three in sequence (reboots in between for disabled)

set -euo pipefail

mode="${1:-enforcing}"
[[ $EUID -eq 0 ]] || { echo "must run as root" >&2; exit 1; }

get_mode() { getenforce 2>/dev/null || echo "disabled"; }

set_mode() {
  case "$1" in
    enforcing)  setenforce 1 ;;
    permissive) setenforce 0 ;;
    disabled)   echo "disabled mode requires /etc/selinux/config edit + reboot"; return 1 ;;
    *) echo "unknown mode: $1" >&2; return 1 ;;
  esac
}

run_smoke_test() {
  local label="$1"
  echo ""
  echo "=== smoke test: $label (current mode: $(get_mode)) ==="

  local fail=0

  if ! systemctl is-active --quiet rustfs-exporter.service; then
    echo "FAIL: rustfs-exporter not active"
    systemctl status rustfs-exporter.service --no-pager | tail -5
    fail=1
  else
    echo "OK: rustfs-exporter active"
  fi

  if ! systemctl is-active --quiet victoria-metrics.service; then
    echo "FAIL: victoria-metrics not active"
    systemctl status victoria-metrics.service --no-pager | tail -5
    fail=1
  else
    echo "OK: victoria-metrics active"
  fi

  if ! curl -sf --max-time 5 http://127.0.0.1:9090/metrics | grep -q '^rustfs_up '; then
    echo "FAIL: rustfs_exporter /metrics not serving rustfs_up"
    fail=1
  else
    echo "OK: rustfs_exporter serves metrics"
  fi

  if ! curl -sf --max-time 5 'http://127.0.0.1:8429/api/v1/query?query=rustfs_up' | grep -q '"value"'; then
    echo "FAIL: VM /api/v1/query returned no value"
    fail=1
  else
    echo "OK: VM query returns data"
  fi

  # Check for AVC denials in last 60s touching our binaries
  local denials
  if command -v ausearch &>/dev/null; then
    denials=$(ausearch -m AVC -ts recent 2>/dev/null | grep -E 'rustfs-exporter|victoria-metrics' | wc -l || true)
    if [[ "$denials" -gt 0 ]]; then
      echo "FAIL: $denials AVC denials recorded (see ausearch output)"
      ausearch -m AVC -ts recent 2>/dev/null | grep -E 'rustfs-exporter|victoria-metrics' | head -5
      fail=1
    else
      echo "OK: no AVC denials"
    fi
  else
    echo "SKIP: ausearch not available (install audit)"
  fi

  return $fail
}

original_mode=$(get_mode)
echo "original SELinux mode: $original_mode"

trap 'echo "restoring SELinux to $original_mode"; set_mode "$original_mode" || true' EXIT

if [[ "$mode" == "all" ]]; then
  for m in enforcing permissive; do
    set_mode "$m"
    run_smoke_test "mode=$m" || echo "TEST FAILED in mode=$m"
  done
  echo ""
  echo "For 'disabled' mode: edit /etc/selinux/config to SELINUX=disabled and reboot."
  echo "After reboot, run: $0 disabled"
else
  set_mode "$mode"
  run_smoke_test "mode=$mode"
fi