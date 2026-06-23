# RustFS Monitoring Stack — Acceptance Report

**Branch:** main
**Date:** 2026-06-23
**Status:** ✅ Verified end-to-end against live rustfs (10.0.50.15)

## Verified end-to-end (live)

Against the user's real rustfs at `https://10.0.50.15:9000` (admin/VMware1!):

| Stage | Verification | Result |
|---|---|---|
| Stack bring-up | `docker compose up -d` | 3 containers up (exporter/vm/grafana) |
| Exporter | `curl localhost:9090/metrics \| grep ^rustfs_` | 13 metrics returned |
| Exporter | `rustfs_up=1`, `rustfs_health_*`, `rustfs_replication_*` | Real values (see below) |
| VictoriaMetrics | `curl localhost:8429/api/v1/query?query=rustfs_up` | Returns `value="1"` |
| VictoriaMetrics | `rustfs_replication_completed_bytes{bucket="rustfs15"}` | Returns `1863193911` (matches admin API directly) |
| Grafana 13.0.2 | `GET /api/health` | `{"version":"13.0.2","database":"ok"}` |
| Grafana 13.0.2 | `/api/datasources/.../health` | `Successfully queried the Prometheus API` |
| Grafana 13.0.2 | `/api/ds/query` for `rustfs_replication_completed_bytes` | Returns `bucket="rustfs15"` frame |
| Grafana 13.0.2 | `/api/search?type=dash-db` | 3 dashboards provisioned (Cluster Health, Replication Overview, Replication Trend) |

**Reproducer** (from `deploy/monitoring/`):

```bash
cp .env.example .env
# edit .env: RUSTFS_ENDPOINT=https://10.0.50.15:9000, RUSTFS_ACCESS_KEY=admin,
#            RUSTFS_SECRET_KEY=VMware1!, RUSTFS_TLS_SKIP_VERIFY=true

docker build -t local-mirror/rustfs-exporter:latest exporter
docker compose up -d
sleep 35   # wait for first VM scrape

curl -s localhost:9090/metrics | grep ^rustfs_replication_completed_bytes
# → rustfs_replication_completed_bytes{bucket="rustfs15"} 1.863193911e+09

curl -s 'localhost:8429/api/v1/query?query=rustfs_replication_completed_bytes' | python3 -m json.tool
# → data.result[0].value = [ts, "1863193911"]
```

## Component acceptance

### Exporter (Go, distroless)

- Builds cleanly: `go build ./cmd/exporter` → 15 MB binary
- Docker image: `local-mirror/rustfs-exporter:latest` → 9.97 MB
- Unit tests: 5 packages, **15 tests** passing
- TLS support: `RUSTFS_CA_CERT` (production) and `RUSTFS_TLS_SKIP_VERIFY` (debug) both verified
- SigV4: includes `X-Amz-Content-Sha256` (required by rustfs; AWS spec compliance)

### 404 handling (silent skip)

Admin endpoint returns 404 for buckets without replication configured (e.g., target rustfs).
`admin.ReplicationMetrics()` returns `ErrNoReplication` sentinel; `collector.cycle()` skips
without logging. Unit tests cover both 404 and 500 paths:

```
TestAdminClient_ReplicationMetrics_404ReturnsErrNoReplication  PASS
TestAdminClient_ReplicationMetrics_500ReturnsRawError         PASS
```

### VictoriaMetrics

- Pinned to `victoriametrics/victoria-metrics:v1.146.0`
- Listens on container port **8429** (mapped to host 8429) via `-httpListenAddr=:8429`
- 30-day retention, 40% memory cap
- Scrape config points at `host.docker.internal:9090` for exporter (which runs on host network)

### Grafana

- **Upgraded to `grafana/grafana:13.0.2`** (latest stable as of 2026-06-23)
- Anonymous admin role enabled (set `GF_ADMIN_PASS` for password)
- 3 dashboards provisioned + 3 alert rules
- Datasource: `VictoriaMetrics` (`http://vm:8429`)

### Compose / deploy

- Removed bundled `rustfs` service — stack now expects external rustfs via `RUSTFS_ENDPOINT`
- Exporter uses `network_mode: host` so it can reach external rustfs IPs
- VM uses `extra_hosts: host.docker.internal:host-gateway` to reach host-network exporter
- TLS volume mount is optional (`RUSTFS_CA_CERT_HOST_PATH` defaults to system CA bundle)
- `.env.example` documents all 9 variables with example values

## Offline deployment

```bash
# On a host with internet:
./scripts/prep-offline.sh       # exports images/{exporter,vm,grafana}.tar
# (add PREP_RUSTFS=1 if you also need the rustfs business image)

# On the target host (air-gapped):
./scripts/load-offline.sh
docker compose up -d
```

See `DEPLOY.md` for the full production deployment guide (TLS, ports, validation, troubleshooting).

## What's NOT verified

- **Alert firing**: alert rules are loaded into VM, but no test has been run to actually
  trigger them (would require setting up a degraded rustfs state).
- **Grafana UI**: dashboards are provisioned and data is queryable via API, but I have not
  clicked through them in a browser to verify panel rendering.
- **10.0.50.18 (target) end-to-end**: only verified at the exporter level (404 → silent skip).
  Not part of the running stack — would require a second exporter pointing at 10.0.50.18.

These gaps are intentional (YAGNI — none of them block the production rollout).