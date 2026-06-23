# RustFS Monitoring Stack — 部署文档

> 一台监控机 + 外部 rustfs 集群（生产推荐）。
> 单机自包含模式（监控机和 rustfs 同一台机器）也支持，只需把 `RUSTFS_ENDPOINT` 指 `http://localhost:9000`。

## 1. 架构

```
   rustfs 集群（外部）
       │  S3 + admin API（端口 9000）
       ▼
┌─────────────────┐    /metrics     ┌──────────────┐     PromQL      ┌─────────┐
│ rustfs-exporter │ ───────────────▶│ VictoriaMtrcs│ ───────────────▶│ Grafana │
│  (distroless)   │    :9090       │  v1.146.0    │     :8429       │ 13.0.2  │
└─────────────────┘                └──────────────┘                 └─────────┘
                                                                            :3000
```

**3 个容器**：exporter / vm / grafana。RustFS 不在栈内，由 `RUSTFS_ENDPOINT` 指向。

**镜像**：
| 容器 | 镜像 | 大小（参考） |
|---|---|---|
| rustfs-exporter | local-mirror/rustfs-exporter:latest | ~10 MB（distroless） |
| victoria-metrics | victoriametrics/victoria-metrics:v1.146.0 | ~30 MB |
| grafana | grafana/grafana:13.0.2 | ~300 MB |

## 2. 快速开始（在线 / 有网）

```bash
cd deploy/monitoring

# 1) 配 .env
cp .env.example .env
$EDITOR .env           # 至少改 RUSTFS_ENDPOINT / RUSTFS_ACCESS_KEY / RUSTFS_SECRET_KEY

# 2) 构建 exporter 镜像（仅首次）
docker build -t local-mirror/rustfs-exporter:latest exporter

# 3) 启动
docker compose up -d

# 4) 验证
curl -s localhost:9090/metrics | grep ^rustfs_ | head -20
curl -s 'localhost:8429/api/v1/query?query=rustfs_up' | head -c 200
```

Grafana 访问 `http://<host>:3000`，默认账号 `admin / admin`（改 `GF_ADMIN_PASS`）。

## 3. 离线部署（无网环境）

### 3.1 在有网的机器上准备 tar

```bash
cd deploy/monitoring
docker build -t local-mirror/rustfs-exporter:latest exporter
./scripts/prep-offline.sh
# 输出 images/{exporter,vm,grafana}.tar
```

若**自包含 rustfs**（监控机和 rustfs 同一台且需要 docker load rustfs 镜像），加 `PREP_RUSTFS=1`：

```bash
PREP_RUSTFS=1 ./scripts/prep-offline.sh
```

### 3.2 在目标机器上加载

```bash
cd deploy/monitoring
cp .env.example .env && $EDITOR .env

./scripts/load-offline.sh          # docker load -i images/*.tar

docker compose up -d
```

### 3.3 私有 CA 证书挂载（生产 HTTPS 必需）

distroless 镜像无系统 CA bundle，**生产环境必须**挂载私有 CA：

```bash
# 假设 CA 证书已保存到 /etc/ssl/certs/rustfs-ca.pem
echo 'RUSTFS_TLS_SKIP_VERIFY=' >> .env
echo 'RUSTFS_CA_CERT_HOST_PATH=/etc/ssl/certs/rustfs-ca.pem' >> .env
docker compose restart exporter
```

**临时调试**（不推荐生产用，MITM 风险）：

```bash
echo 'RUSTFS_TLS_SKIP_VERIFY=true' >> .env
echo 'RUSTFS_CA_CERT_HOST_PATH=/etc/hostname' >> .env
docker compose restart exporter
```

## 4. 配置项参考

### 4.1 exporter env vars

| 变量 | 必填 | 默认 | 说明 |
|---|---|---|---|
| `RUSTFS_ENDPOINT` | ✅ | — | 含协议头；admin 与 S3 同址同端口 |
| `RUSTFS_ACCESS_KEY` | ✅ | — | admin API 复用 |
| `RUSTFS_SECRET_KEY` | ✅ | — | admin API 复用 |
| `RUSTFS_REGION` | | `us-east-1` | SigV4 必需，rustfs 不校验值 |
| `RUSTFS_EXPORTER_LISTEN` | | `:9090` | exporter HTTP 端口 |
| `RUSTFS_EXPORTER_SCRAPE_INTERVAL` | | `30s` | exporter → rustfs 的抓取周期 |
| `RUSTFS_TLS_SKIP_VERIFY` | | `false` | 跳过证书验证（仅调试） |
| `RUSTFS_CA_CERT` | | `/etc/rustfs/ca.pem` | 容器内 CA 证书路径 |
| `RUSTFS_CA_CERT_HOST_PATH` | | `/etc/ssl/certs/ca-certificates.crt` | 宿主机 CA 证书路径（compose 挂载源） |

### 4.2 端口

| 端口 | 服务 | 暴露 |
|---|---|---|
| 9000 | rustfs | **不暴露**（在 rustfs 主机上） |
| 9090 | rustfs-exporter | 是（供 VM / 外部 scrape） |
| 8429 | VictoriaMetrics | 是（PromQL + UI） |
| 3000 | Grafana | 是（Web UI） |

## 5. 验证 e2e

```bash
# 1. exporter 在线
curl -s localhost:9090/healthz   # → ok
curl -s localhost:9090/metrics | grep -c '^rustfs_'   # → 至少 11 个

# 2. VM 抓到 exporter
curl -s 'localhost:8429/api/v1/query?query=rustfs_up' | jq '.data.result | length'
# → 应 ≥ 1（第一次 scrape 还没数据，最多等 30s）

# 3. Grafana 面板可见
curl -s -u admin:admin localhost:3000/api/search?type=dash-db | jq '.[].title'
# → 应包含 "RustFS / Health"、"RustFS / Replication Overview"、"RustFS / Replication Trend"

# 4. 报警规则加载
curl -s localhost:8429/api/v1/rules | jq '.data.groups[].name'
# → 应包含 "rustfs"

# 5. 复制桶的真实指标
curl -s localhost:9090/metrics | grep rustfs_replication_completed_bytes
# → rustfs_replication_completed_bytes{bucket="<your-bucket>"} <bytes>
```

## 6. 行为说明

### 6.1 404 是正常状态

exporter 对**每个桶**调用 `GET /rustfs/admin/v3/replicationmetrics?bucket=<name>`。
如果桶没有配置跨区域复制（即它是目标端，或源端未配复制），admin 返回 404。
**这种情况下 exporter 不输出 replication 指标**，stderr 也不会有错误日志（自 v1 起 404 静默跳过）。

如果看到 stderr 出现 `replication <bucket>: status 5xx`，那才是真错误——查 admin 服务状态。

### 6.2 `rustfs_up` 含义

| 值 | 含义 |
|---|---|
| `1` | S3 `ListBuckets` 调用成功，exporter 与 rustfs 通信正常 |
| `0` | S3 调用失败（网络、认证、rustfs 不可达），所有指标无意义 |

### 6.3 抓取频率

```
exporter → rustfs   : RUSTFS_EXPORTER_SCRAPE_INTERVAL (默认 30s)
vm → exporter       : conf/vm-scrape.yml scrape_interval (默认 30s)
```

两者都改 30s 是合适的：复制延迟通常以分钟计，10s 太密也看不出趋势。

## 7. 故障排查

| 现象 | 原因 | 修复 |
|---|---|---|
| `rustfs_up 0` | 凭证错 / 网络不通 / rustfs 9000 端口未暴露 | `curl -k -u ak:sk https://host:9000/` |
| `curl: (60) SSL certificate problem` | distroless 无系统 CA | 配 `RUSTFS_CA_CERT_HOST_PATH` 或 `RUSTFS_TLS_SKIP_VERIFY=true` |
| `missing header: x-amz-content-sha256` | 极少见，exporter 旧版本 | 升级镜像（v1 起已修） |
| VM 一直无数据 | exporter 没起来 / scrape 路径错 | `curl localhost:9090/metrics` 先确认 |
| Grafana 面板空 | 数据源没配好 | Administration → Data sources → VictoriaMetrics → Save & test |
| 容器起不来：`bind: path does not exist` | `RUSTFS_CA_CERT_HOST_PATH` 指向不存在的文件 | 改成主机上确实存在的路径（即便不读） |

## 8. 生产 checklist

- [ ] `RUSTFS_TLS_SKIP_VERIFY=false`（必须）
- [ ] `RUSTFS_CA_CERT_HOST_PATH` 指向私有 CA（不要 skip-verify）
- [ ] `GF_ADMIN_PASS` 改了默认 `admin`
- [ ] 防火墙：仅暴露 9090/8429/3000 给内网
- [ ] VM 存储：`vm-data` volume 用单独磁盘，防 OOM 拖垮业务
- [ ] Grafana 持久化：`grafana-data` volume 单独磁盘
- [ ] 报警接收人：改 `conf/grafana-alerts.yaml` 的 `notification_channel`
- [ ] rustfs 凭证：用**只读**运维账号而非 root 账号