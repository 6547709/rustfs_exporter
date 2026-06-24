# Deployment Guide

Three deployment modes are supported. Pick the one that matches your topology:

- **[§1 Docker Compose](#1-docker-compose)** — single host, dev/test
- **[§2 systemd](#2-systemd-rhel-9--rocky-9)** — production host, hardened, SELinux-friendly
- **[§3 OpenShift Grafana](#3-openshift-grafana-remote)** — Grafana on remote OpenShift cluster, scraping VM via network

---

## 1. Docker Compose

Single host with all three services. Quick start, good for dev/test.

### 1.1 Quick start

```bash
cd deploy/monitoring
cp .env.example .env
$EDITOR .env   # set RUSTFS_ENDPOINT / RUSTFS_ACCESS_KEY / RUSTFS_SECRET_KEY

docker build -t local-mirror/rustfs-exporter:latest exporter
docker compose up -d
```

### 1.2 Architecture

```
rustfs cluster (external)
    │
    └─→ exporter (host network) :9090  ──→ VictoriaMetrics :8429  ──→ Grafana :3000
```

The exporter uses `network_mode: host` so it can reach external rustfs IPs.
VictoriaMetrics uses `extra_hosts: host.docker.internal:host-gateway` to
scrape the host-network exporter.

### 1.3 Configuration

See [`.env.example`](./.env.example) for all 9 environment variables.

TLS options:
- **Production**: set `RUSTFS_CA_CERT_HOST_PATH` to a real CA bundle file
- **Dev only**: set `RUSTFS_TLS_SKIP_VERIFY=true`

### 1.4 Ports

| Port | Service | Exposed |
|---|---|---|
| 9000 | rustfs (external) | — |
| 9090 | rustfs-exporter | yes (host) |
| 8429 | VictoriaMetrics | yes (host) |
| 3000 | Grafana | yes (host) |

### 1.5 Verification

```bash
curl -s localhost:9090/metrics | grep ^rustfs_up
curl -s 'localhost:8429/api/v1/query?query=rustfs_up'
curl -s -u admin:admin localhost:3000/api/health   # → Grafana version
curl -s -u admin:admin localhost:3000/api/search?type=dash-db   # → ["RustFS"]
```

### 1.6 Offline deployment

```bash
# On an internet-connected host:
cd deploy/monitoring
docker build -t local-mirror/rustfs-exporter:latest exporter
./scripts/prep-offline.sh       # exports images/{exporter,vm,grafana}.tar

# On the target host (air-gapped):
cd deploy/monitoring
cp .env.example .env && $EDITOR .env
./scripts/load-offline.sh       # docker load -i images/*.tar
docker compose up -d
```

### 1.7 Behavior notes

**404 from admin endpoint is normal.** For buckets without replication
configured (target rustfs, or source without rules), admin returns 404 and
the exporter silently skips the bucket (no log, no metric for that bucket).
If you see `replication <bucket>: status 5xx` in stderr, that is a real error.

**`rustfs_up = 0`** means the exporter cannot reach rustfs (network or auth).

---

## 2. systemd (RHEL 9 / Rocky 9)

Production host with native Linux services. Hardened sandbox, SELinux-friendly.

### 2.1 What gets installed

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
| User `rustfs-mon` | system user |

### 2.2 Install

See [`systemd/README.md`](./systemd/README.md) for the full guide. Quick version:

```bash
cd deploy/monitoring

# Build static exporter binary
cd exporter
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../systemd/rustfs-exporter ./cmd/exporter
cd ..

# Download VM tarball
curl -fsSL -o systemd/vm.tgz \
  https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/v1.146.0/victoria-metrics-linux-amd64-v1.146.0.tar.gz

# Fill in config
cp systemd/env/rustfs-exporter.env.example /tmp/exporter.env
$EDITOR /tmp/exporter.env

sudo EXPORTER_BIN=exporter/rustfs-exporter \
     VM_TARBALL=systemd/vm.tgz \
     EXPORTER_ENV=/tmp/exporter.env \
     systemd/install.sh
```

### 2.3 SELinux

Tested for enforcing / permissive / disabled on Rocky 9. The installer places
binaries under `/usr/local/bin` (default `bin_t`, runs in `unconfined_t`) and
state under `/var/lib` + `/etc` (default labels). Standard RHEL targeted
policy allows all of this; the installer also runs `restorecon` as a safety net.

Verify:

```bash
sudo systemd/tests/selinux.sh enforcing
sudo systemd/tests/selinux.sh permissive
# 'disabled' requires editing /etc/selinux/config + reboot
```

### 2.4 Operations

```bash
sudo systemctl status rustfs-exporter victoria-metrics
sudo journalctl -u rustfs-exporter -f
sudo journalctl -u victoria-metrics -f
curl -s localhost:9090/metrics | grep ^rustfs_
curl -s 'localhost:8429/api/v1/query?query=rustfs_up'
```

For Grafana, run it on this host (Docker Compose, or a separate Grafana install)
or deploy it to OpenShift per §3.

### 2.5 Uninstall

```bash
sudo systemd/uninstall.sh            # keep config + data
sudo systemd/uninstall.sh --purge-data  # also delete state + user
```

---

## 3. OpenShift Grafana (remote)

Deploy Grafana on a remote OpenShift 4.x cluster. The cluster's egress must
be able to reach a VictoriaMetrics instance (running either via Docker
Compose or systemd on a different host).

### 3.1 Network prerequisites

The OpenShift cluster's egress CIDR must be allowed to reach the VM host on
port 8429. Get the cluster's egress range and add a firewall rule on the VM host:

```bash
# On the OpenShift cluster:
oc get clusteroperator network -o jsonpath='{.status.conditions[?(@.type=="PodNetwork01")].message}'
# typically something like 10.128.0.0/14 — adjust as needed

# On the VM host:
firewall-cmd --zone=public --add-rich-rule='
  rule family=ipv4 source address=<OPENSHIFT_EGRESS_CIDR> \
  port port=8429 protocol=tcp accept'
```

### 3.2 Deploy

See [`openshift/README.md`](./openshift/README.md) for the full guide. Quick version:

```bash
cd deploy/monitoring/openshift

# 1. Generate Grafana admin password
GRAFANA_PASS=$(openssl rand -base64 24 | tr -d '\n=' | head -c 32)

# 2. Replace placeholder in kustomization.yaml
sed -i "s/REPLACE_WITH_RANDOM_STRING/$GRAFANA_PASS/" kustomization.yaml

# 3. Set the remote VM URL
sed -i "s|\${VM_REMOTE_URL}|http://vm-host.example.com:8429|" config-datasource.yaml

# 4. Apply
oc new-project rustfs-monitoring
oc apply -k .

# 5. Wait + verify
oc rollout status deploy/grafana -n rustfs-monitoring
ROUTE=$(oc get route grafana -n rustfs-monitoring -o jsonpath='{.spec.host}')
echo "https://$ROUTE"
```

### 3.3 What gets deployed

| Resource | Purpose |
|---|---|
| `Namespace/rustfs-monitoring` | dedicated namespace |
| `ServiceAccount/grafana` | pod identity |
| `Deployment/grafana` | Grafana 13.0.2, rootless, hardened |
| `Service/grafana` | ClusterIP :3000 |
| `Route/grafana` | edge TLS |
| `ConfigMap/grafana-datasources` | VictoriaMetrics datasource |
| `ConfigMap/grafana-dashboard-rustfs` | the single `RustFS` dashboard JSON |
| `ConfigMap/grafana-alerts` | 3 alert rules |
| `Secret/grafana-admin` | admin password |

### 3.4 Verification

```bash
oc rsh -n rustfs-monitoring deploy/grafana \
  curl -s http://localhost:3000/api/health
# → {"version":"13.0.2","database":"ok"}

oc rsh -n rustfs-monitoring deploy/grafana \
  curl -s -u admin:$GRAFANA_PASS \
    http://localhost:3000/api/datasources/uid/PBFA97CFB590B2093/health
# → "Successfully queried the Prometheus API"
```

### 3.5 Uninstall

```bash
oc delete -k openshift/
oc delete namespace rustfs-monitoring
```

---

## Common tasks

### Update Grafana admin password

| Mode | How |
|---|---|
| Compose | edit `GF_ADMIN_PASS` in `.env`, `docker compose up -d grafana` |
| systemd | n/a (Grafana not deployed here) |
| OpenShift | `oc patch secret grafana-admin -p '{"data":{"password":"'$(echo -n newpass \| base64)'"}}'` |

### Rotate rustfs credentials

| Mode | How |
|---|---|
| Compose | edit `.env`, `docker compose restart exporter` |
| systemd | edit `/etc/rustfs-mon/exporter.env`, `sudo systemctl restart rustfs-exporter` |

### View metrics from any host

```bash
# Exporter
curl http://<host>:9090/metrics | grep ^rustfs_

# VictoriaMetrics (PromQL)
curl 'http://<host>:8429/api/v1/query?query=rustfs_replication_completed_bytes'
```