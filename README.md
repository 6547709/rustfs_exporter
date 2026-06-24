# RustFS Monitoring Stack

Prometheus exporter + VictoriaMetrics + Grafana, packaged for three deployment modes:

| Mode | Use case | Where |
|---|---|---|
| **Docker Compose** | local dev, single host, quick start | [§1](./DEPLOY.md#1-docker-compose) |
| **systemd** | RHEL 9 / Rocky 9 production host, hardened, SELinux-friendly | [§2](./DEPLOY.md#2-systemd-rhel-9--rocky-9) |
| **OpenShift Grafana** | remote Grafana on OpenShift 4.x, scrapes VM from a different host | [§3](./DEPLOY.md#3-openshift-grafana-remote) |

## Quick links

- **Design doc**: `../../docs/architecture/rustfs-monitoring-design.md`
- **e2e acceptance**: [ACCEPTANCE.md](./ACCEPTANCE.md)
- **Exporter source**: [`exporter/`](./exporter/) — Go module `github.com/local/rustfs-exporter`

## Repository layout

```
deploy/monitoring/
├── docker-compose.yml          # compose stack (exporter + vm + grafana)
├── .env.example                # 9 vars template
├── conf/                       # Grafana datasource + dashboards provider + alerts + VM scrape
├── dashboards/rustfs.json      # single consolidated Grafana dashboard
├── exporter/                   # Go static-binary exporter (CGO_ENABLED=0, distroless)
├── systemd/                    # native Linux services (RHEL 9 / Rocky 9)
├── openshift/                  # OpenShift Grafana manifests (Kustomize)
├── scripts/                    # offline prep/load for compose
├── ACCEPTANCE.md               # live e2e verification report
├── DEPLOY.md                   # full deployment guide (3 modes)
└── README.md                   # this file
```