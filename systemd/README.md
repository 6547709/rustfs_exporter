# systemd deployment — rustfs-exporter + VictoriaMetrics

Install both services as native Linux systemd units on a RHEL 9 / Rocky 9 host.
The exporter serves `/metrics` on port 9090; VictoriaMetrics scrapes it and serves
PromQL on port 8429.

Grafana is **not** installed by this stack — see `../openshift/` for the remote
OpenShift deployment, or run Grafana via the Docker Compose stack in `../`.

## Quick start

```bash
# 1. Build the static exporter binary (from repo root)
cd ../exporter
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../systemd/rustfs-exporter ./cmd/exporter

# 2. Download VictoriaMetrics tarball
cd ../systemd
curl -fsSL -o vm.tgz \
  https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/v1.146.0/victoria-metrics-linux-amd64-v1.146.0.tar.gz
# (for arm64, use victoria-metrics-linux-arm64-v1.146.0.tar.gz)

# 3. Fill in env vars
cp env/rustfs-exporter.env.example /tmp/exporter.env
$EDITOR /tmp/exporter.env   # set RUSTFS_ENDPOINT / ACCESS_KEY / SECRET_KEY

# 4. Install as root
sudo EXPORTER_BIN=../exporter/rustfs-exporter \
     VM_TARBALL=vm.tgz \
     EXPORTER_ENV=/tmp/exporter.env \
     ./install.sh

# 5. Verify
sudo systemctl status rustfs-exporter victoria-metrics
curl -s localhost:9090/metrics | grep ^rustfs_up
curl -s 'localhost:8429/api/v1/query?query=rustfs_up'
```

## What gets installed

| Path | Purpose |
|---|---|
| `/usr/local/bin/rustfs-exporter` | exporter binary |
| `/usr/local/bin/victoria-metrics` | VM binary |
| `/etc/systemd/system/rustfs-exporter.service` | unit |
| `/etc/systemd/system/victoria-metrics.service` | unit |
| `/etc/rustfs-mon/exporter.env` | exporter config (sensitive) |
| `/etc/rustfs-mon/victoria-metrics.env` | VM config (sensitive) |
| `/etc/rustfs-mon/victoria-metrics/scrape.yml` | VM scrape config |
| `/var/lib/rustfs-mon` | working directory |
| `/var/lib/victoria-metrics` | VM data dir |
| User `rustfs-mon` | system user (no shell, no home login) |

## SELinux

The installer places binaries under `/usr/local/bin` (default label `bin_t`,
runs in `unconfined_t`) and state under `/var/lib` + `/etc` (default labels
`var_lib_t`, `etc_t`). Standard RHEL targeted policy allows all of this.

The installer also runs `restorecon -R` on every directory it creates as a
belt-and-suspenders measure.

**Verification on a Rocky 9 / RHEL 9 host**:

```bash
sudo ./tests/selinux.sh enforcing   # most strict
sudo ./tests/selinux.sh permissive  # logs denials but doesn't enforce
# For 'disabled': edit /etc/selinux/config → SELINUX=disabled, reboot,
# then run ./tests/selinux.sh disabled (script skips mode change)
```

The test script checks that both services are `active`, the metrics endpoint
returns `rustfs_up`, and `ausearch` reports zero AVC denials involving our
binaries.

## Custom SELinux confinement (optional, advanced)

If your site policy requires our binaries to run in a confined domain rather
than `unconfined_t`, drop a `.te` module that confines `/usr/local/bin/rustfs-exporter`
and `/usr/local/bin/victoria-metrics` (allow network connect/bind to ports
9090 and 8429; allow reading the env file and scrape config). Build with
`checkmodule -M -o mod.mod conf.te && semodule_package -o mod.pp -m mod.mod && semodule -i mod.pp`.

This is out of scope for the default install — most sites do not need it.

## Hardening

Both units apply systemd's modern sandbox directives:

- `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`
- `PrivateDevices`, `ProtectClock`, `ProtectKernelTunables`, `ProtectKernelModules`
- `RestrictSUIDSGID`, `RestrictNamespaces`, `RestrictRealtime`
- `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`, `SystemCallArchitectures=native`
- `LockPersonality`, `MemoryDenyWriteExecute`
- `CapabilityBoundingSet=` (empty)

VM additionally needs `ReadWritePaths=/var/lib/victoria-metrics` because of
its data dir. The exporter writes nothing.

Resource limits:
- exporter: `MemoryMax=256M`, `TasksMax=2048`
- VM: `MemoryMax=2G`, `TasksMax=4096` (plus the in-VM `-memory.allowedPercent=40`)

## Networking

Both services bind on all interfaces by default (`*:9090` and `*:8429`).
If the host is on a network where the OpenShift Grafana pod scrapes VM directly,
restrict 8429 at the firewall to the cluster's egress IP range.

Or enable VM HTTP basic auth by setting `VM_HTTP_AUTH_USER` / `VM_HTTP_AUTH_PASSWORD`
in `/etc/rustfs-mon/victoria-metrics.env` and editing the unit's `ExecStart` to
add `-httpAuth.username=${VM_HTTP_AUTH_USER} -httpAuth.password=${VM_HTTP_AUTH_PASSWORD}`.

## Uninstall

```bash
sudo ./uninstall.sh            # keep config + data
sudo ./uninstall.sh --purge-data  # also delete state dirs and user
```

## Logs

```bash
sudo journalctl -u rustfs-exporter -f
sudo journalctl -u victoria-metrics -f
```