# RustFS Prometheus Exporter + Monitoring Stack

抓取 RustFS S3 API + admin API，导出 Prometheus 指标，可视化用 VictoriaMetrics + Grafana 13。

**13 个指标**，按桶分维度，覆盖：
- 集群健康（storage / iam / lock 三个组件）
- 复制状态（pending / completed / failed / 带宽 / 队列）

**3 种部署模式**：Docker Compose（开发）、systemd（生产主机）、OpenShift Grafana（远程 Grafana）。

---

## 5 分钟上手

### 选哪种部署模式？

```
你有什么机器？
├── 单机测试 → docker compose（§1）
├── 生产主机 (RHEL/Rocky) → systemd（§2）
└── 已有 OpenShift 集群，只跑 Grafana → openshift（§3）
```

### 最快路径（docker compose）

```bash
git clone https://github.com/6547709/rustfs_exporter.git
cd rustfs_exporter
cp .env.example .env && vim .env       # 改 RUSTFS_ENDPOINT + 凭证
docker build -t local-mirror/rustfs-exporter:latest exporter
docker compose up -d
sleep 35
curl -s localhost:9090/metrics | grep ^rustfs_up
xdg-open http://localhost:3000          # admin / admin
```

详见 [`DEPLOY.md`](./DEPLOY.md)。

---

## 文档地图

| 文档 | 内容 |
|---|---|
| **[`DEPLOY.md`](./DEPLOY.md)** | 完整部署指南（3 种模式 × 13 步） |
| **[`STORAGE.md`](./STORAGE.md)** | 数据存哪里、改保留时间、备份迁移 |
| **[`ACCEPTANCE.md`](./ACCEPTANCE.md)** | e2e 验收报告（10.0.50.15 真实环境） |
| [`systemd/README.md`](./systemd/README.md) | systemd 模式速查 |
| [`openshift/README.md`](./openshift/README.md) | OpenShift 模式速查 + 网络要求 |

---

## 仓库结构

```
rustfs_exporter/
├── README.md                     # 本文件
├── DEPLOY.md                     # 完整部署指南
├── STORAGE.md                    # 数据存储和保留配置
├── ACCEPTANCE.md                 # e2e 验收报告
├── docker-compose.yml            # Compose 部署
├── .env.example                  # 环境变量模板
├── .github/workflows/ci.yml      # CI: test + tag-only build + release
│
├── conf/                         # Grafana + VM 配置
│   ├── grafana-datasources.yaml  # VictoriaMetrics 数据源（UID pinned）
│   ├── grafana-dashboards.yaml   # 仪表板 provider
│   ├── grafana-alerts.yaml       # 3 个报警规则
│   └── vm-scrape.yml             # VM scrape 配置
│
├── dashboards/
│   └── rustfs.json               # 单仪表板（health + overview + trend 合并）
│
├── exporter/                     # Go 静态二进制 exporter
│   ├── cmd/exporter/main.go
│   ├── internal/{collector,config,metrics,rustfs,sigv4}/
│   ├── Dockerfile                # distroless 多阶段构建
│   └── go.mod
│
├── systemd/                      # systemd 部署（生产 RHEL/Rocky 9）
│   ├── README.md
│   ├── install.sh                # 幂等安装脚本
│   ├── uninstall.sh
│   ├── env/*.env.example         # 配置模板
│   ├── etc/*.service             # systemd unit（硬化）
│   ├── etc/victoria-metrics/scrape.yml.example
│   └── tests/selinux.sh          # SELinux 三态自动测试
│
├── openshift/                    # OpenShift Grafana 部署
│   ├── README.md
│   ├── kustomization.yaml
│   ├── namespace.yaml
│   ├── grafana-{deployment,service,route}.yaml
│   ├── config-{datasource,dashboards-cfg,alerts}.yaml
│   └── dashboards-rustfs.yaml    # 仪表板 JSON 嵌入 ConfigMap
│
└── scripts/                      # 离线镜像 prep/load（compose 用）
    ├── prep-offline.sh
    └── load-offline.sh
```

---

## 指标清单

13 个指标，所有复制/健康指标都带 `cluster` 标签（区分多个 rustfs 实例）+ `bucket` 标签：

| 指标 | 类型 | 单位 | 标签 | 含义 |
|---|---|---|---|---|
| `rustfs_up` | gauge | 0/1 | — | exporter 上次抓取是否成功 |
| `rustfs_health_ready` | gauge | 0/1 | `cluster`, `component` | storage/iam/lock 是否就绪 |
| `rustfs_replication_pending_bytes` | gauge | bytes | `cluster`, `bucket` | 当前待复制字节数 |
| `rustfs_replication_pending_count` | gauge | objects | `cluster`, `bucket` | 当前待复制对象数 |
| `rustfs_replication_completed_bytes` | gauge | bytes | `cluster`, `bucket` | **累计**复制字节数（counter，用 `rate()` 取吞吐） |
| `rustfs_replication_completed_count` | gauge | objects | `cluster`, `bucket` | **累计**复制对象数 |
| `rustfs_replication_failed_count` | gauge | objects | `cluster`, `bucket` | **累计**失败对象数 |
| `rustfs_replication_bandwidth_current_bytes` | gauge | bytes/sec | `cluster`, `bucket` | 当前瞬时带宽 |
| `rustfs_replication_queue_current_bytes` | gauge | bytes | `cluster`, `bucket` | 当前队列字节数 |
| `rustfs_replication_queue_last_minute_bytes` | gauge | bytes | `cluster`, `bucket` | 过去 1 分钟平均队列字节数 |
| `rustfs_replication_queue_max_bytes` | gauge | bytes | `cluster`, `bucket` | 启动以来最大队列字节数 |

> `cluster` 标签是多 rustfs 部署时用来区分源/目标（值来自 `RUSTFS_TARGETS_JSON` 里每个目标的 `name`）。
> 用 `instance` 标签会和 Prometheus 约定的 scrape target 标签冲突，所以这里用 `cluster`。

单位换算在 Grafana 仪表板里自动处理（`1863193911` 显示为 `1.74 GiB`）。

---

## 常用命令速查

```bash
# 检查 exporter 在线
curl -s localhost:9090/metrics | grep ^rustfs_up

# 用 PromQL 查 VM
curl -s 'localhost:8429/api/v1/query?query=rustfs_up'

# 查看 Grafana 仪表板（API）
curl -s -u admin:admin localhost:3000/api/search?type=dash-db

# 查看 systemd 服务日志
sudo journalctl -u rustfs-exporter -f
```

---

## License

[MIT](./LICENSE) (待添加) — 当前为初始版本，待用户确认许可证。