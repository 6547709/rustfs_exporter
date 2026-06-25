# Deployment Guide — rustfs_exporter

> RustFS 监控栈的完整部署文档。三种部署模式都覆盖。
> **选哪种模式？** 看完第 0 节再选。

---

## 0. 选择部署模式

```
                你有什么？
                    │
        ┌───────────┴────────────┐
        ▼                        ▼
  一台新机器                 已有 K8s/OpenShift
        │                        │
        ▼                        ▼
  仅本地开发测试?           Grafana 跑在哪?
        │                        │
   ┌────┴────┐              ┌────┴────┐
   ▼         ▼              ▼         ▼
 docker    systemd         同集群    远程集群
 compose  (RHEL/Rocky)     上跑     (如 OpenShift)
                         → 用
                        openshift/
                        (Grafana
                         only)
```

| 模式 | 适用 | 文件 |
|---|---|---|
| **docker compose** | 本地测试、单主机快速验证 | `docker-compose.yml` |
| **systemd** | 生产主机（RHEL 9 / Rocky 9 / Ubuntu 22.04+） | `systemd/` |
| **openshift** | Grafana 跑在远端 OpenShift 集群，VM 跑在另外主机 | `openshift/` |

---

## 0.5 数据源 — exporter 怎么抓数据

**Exporter 通过同一个 `RUSTFS_ENDPOINT` 调 3 个 rustfs HTTP 端点：**

| # | 端点 | 用途 | 凭证 |
|---|---|---|---|
| 1 | `GET /` (S3 ListAllMyBuckets) | 列所有桶 | SigV4 签名（`RUSTFS_ACCESS_KEY` / `RUSTFS_SECRET_KEY`）|
| 2 | `GET /rustfs/admin/v3/replicationmetrics?bucket=<name>` | 每个桶的复制指标（pending/completed/failed/bandwidth/queue）| 同 1（admin 复用 S3 凭证）|
| 3 | `GET /health/ready` | 集群组件健康（storage/iam/lock）| **公共端点，不签名**|

**为什么不直接用 Prometheus `servers` 抓 rustfs 自身的 `/metrics` 端点？**
- rustfs 服务本身**不暴露** `/metrics` 端点（截至 v1.146）
- rustfs 暴露的是 admin API JSON，需要解析 JSON 后转成 Prometheus 格式
- 所以本项目**写了一个专用 exporter**（不是 generic scraper），Go 静态二进制 + distroless

**多 rustfs 抓取**：通过 `RUSTFS_TARGETS_JSON` 配置多个 endpoint（每个一组独立凭证），exporter 在每个 scrape 周期对所有 target 串行调用 3 个端点，导出 13 个 metric × N 个 target。

源码：
- `exporter/internal/rustfs/s3.go` — S3 ListBuckets
- `exporter/internal/rustfs/admin.go` — admin API 调用 + JSON 解析

**已知数据源问题（rustfs 端 bug）**：

`/rustfs/admin/v3/replicationmetrics` 里的 `current_bandwidth_bytes_per_sec` 字段**不可信**：

- 小流量时 under-report ~100 倍（实际 100 B/s 时它报 1 B/s）
- 大流量时 over-report ~200 倍（实际 100 MB/s 时它报 20 GB/s）

**应对**：
- Dashboard 不直接用这个 metric 显示带宽，改用 `irate(completed_bytes[5m])` 自己算真实吞吐量
- 原 metric 仍导出但 panel 标为 "Reported Bandwidth (UNRELIABLE)"
- 真正的峰值用 `max_over_time(irate(...)[1h:1m])` 算过去 1 小时最高

---

## 1. docker compose（最快上手）

**适用**：单机测试、demo、CI 环境。

**架构**：本机起 3 个容器（exporter + VictoriaMetrics + Grafana 13.0.2），对外暴露 9090/8429/3000。

### 1.1 前置要求

| 工具 | 最低版本 | 检查命令 |
|---|---|---|
| Docker Engine | 20.10+ | `docker --version` |
| Docker Compose | v2 (插件) | `docker compose version` |
| 联网（首次拉镜像） | — | — |

### 1.2 部署步骤

```bash
# 第 1 步：进入项目目录
cd deploy/monitoring

# 第 2 步：创建 .env 文件（复制模板）
cp .env.example .env

# 第 3 步：编辑 .env，填入你 rustfs 的真实凭证
#    至少改这 3 行：
#      RUSTFS_ENDPOINT=https://你的rustfs:9000
#      RUSTFS_ACCESS_KEY=你的访问密钥
#      RUSTFS_SECRET_KEY=你的密钥
vim .env     # 或 nano / VSCode

# 第 4 步：构建 exporter 镜像（约 30 秒）
docker build -t local-mirror/rustfs-exporter:latest exporter

# 第 5 步：起服务（后台）
docker compose up -d

# 第 6 步：等 35 秒让 VictoriaMetrics 第一次 scrape 完成
sleep 35
```

### 1.3 验证

```bash
# 1) exporter 在工作（暴露 13 个 rustfs_* 指标）
curl -s localhost:9090/metrics | grep ^rustfs_ | head -5
# 期望看到：
#   rustfs_health_ready{component="storage"} 1
#   rustfs_replication_completed_bytes{bucket="rustfs15"} 1.86...
#   rustfs_up 1

# 2) VictoriaMetrics 抓到 exporter 数据
curl -s 'localhost:8429/api/v1/query?query=rustfs_up'
# 期望返回 value=[时间戳, "1"]

# 3) Grafana 13.0.2 健康
curl -s -u admin:admin localhost:3000/api/health
# 期望返回：{"version":"13.0.2","database":"ok"}

# 4) 仪表板已加载（应该看到 1 个）
curl -s -u admin:admin 'localhost:3000/api/search?type=dash-db'
# 期望返回：[{...,"title":"RustFS","uid":"rustfs-overview"}]

# 5) 报警规则已加载（应该看到 3 个）
curl -s -u admin:admin localhost:3000/api/v1/provisioning/alert-rules | grep title
# 期望看到 3 条：Cluster unhealthy / Replication pending / Exporter down
```

### 1.4 打开 Grafana 看仪表板

浏览器访问 **`http://localhost:3000`**，账号 **`admin / admin`**（或者你 `.env` 里改的 `GF_ADMIN_PASS`）。

仪表板 "RustFS" 会自动出现，包含 14 个面板：
- 第 1 行：4 个 stat（Storage/IAM/Lock/Exporter 健康）
- 第 2 行：每个桶的复制状态表
- 第 3-5 行：6 个时序图（待复制、吞吐量、累计、带宽、失败率、队列）

### 1.5 停掉 / 重启 / 清理

```bash
docker compose stop           # 停，不删
docker compose restart        # 重启（配置变更后用）
docker compose down           # 停 + 删容器（保留数据 volume）
docker compose down -v        # 停 + 删容器 + 删所有数据
```

### 1.6 数据存哪里？

| 容器 | volume 名（自动创建） | 宿主机路径（默认） |
|---|---|---|
| exporter | 无（无状态） | — |
| victoria-metrics | `monitoring_vm-data` | `/var/lib/docker/volumes/monitoring_vm-data/_data` |
| grafana | `monitoring_grafana-data` | `/var/lib/docker/volumes/monitoring_grafana-data/_data` |

**改 VM 数据保留时间**：编辑 `docker-compose.yml` 里 vm 服务的 `-retentionPeriod=30d`（30 天），重启 `docker compose up -d vm`。

---

## 2. systemd（生产主机）

**适用**：RHEL 9 / Rocky Linux 9 / AlmaLinux 9 / Ubuntu 22.04+。原生 systemd 服务 + SELinux 友好。

**架构**：两个 systemd unit（rustfs-exporter + victoria-metrics）跑在主机上。Grafana 单独装（不在本监控栈覆盖范围内；用 docker compose 装、或单独装、或者按 §3 跑在 OpenShift 上）。

### 2.1 前置要求

| 工具 | 最低版本 | 安装命令（RHEL/Rocky） |
|---|---|---|
| systemd | 252+ | 预装 |
| Go（仅构建时） | 1.22+ | `dnf install -y golang` |
| tar / curl | — | 预装 |
| SELinux（推荐） | enforcing | 预装 |
| 用户权限 | root | `sudo -i` |

### 2.2 部署步骤

```bash
# 第 1 步：进入项目目录
cd deploy/monitoring

# 第 2 步：构建静态 exporter 二进制（约 10 秒）
cd exporter
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
  -o ../systemd/rustfs-exporter ./cmd/exporter
cd ..
# 产出：systemd/rustfs-exporter（11 MB 静态二进制）

# 第 3 步：下载 VictoriaMetrics 二进制（约 80 MB）
#    替换架构：amd64 → linux-amd64；arm64 → linux-arm64
VM_VERSION="v1.146.0"
VM_ARCH="amd64"   # 或 arm64
curl -fsSL -o systemd/victoria-metrics \
  "https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/${VM_VERSION}/victoria-metrics-linux-${VM_ARCH}-${VM_VERSION}"
chmod +x systemd/victoria-metrics
ls -lh systemd/victoria-metrics   # 应约 80 MB
```

### 2.3 填配置文件

```bash
# 第 4 步：复制 exporter 配置模板
cp systemd/env/rustfs-exporter.env.example /tmp/exporter.env

# 第 5 步：编辑 /tmp/exporter.env，改这 3 行：
#      RUSTFS_ENDPOINT=https://你的rustfs:9000
#      RUSTFS_ACCESS_KEY=你的密钥
#      RUSTFS_SECRET_KEY=你的密钥
#    还要确认：
#      RUSTFS_CA_CERT=/etc/pki/tls/certs/ca-bundle.crt   # RHEL 系统 CA bundle
#      RUSTFS_TLS_SKIP_VERIFY=false                     # 生产不要跳过
vim /tmp/exporter.env
```

### 2.4 安装

```bash
# 第 6 步：安装（二进制 + 用户 + 目录 + systemd unit + 配置 + restorecon）
sudo EXPORTER_BIN="$PWD/systemd/rustfs-exporter" \
     EXPORTER_ENV=/tmp/exporter.env \
     systemd/install.sh
# 这个脚本做了：
#   - 创建系统用户 rustfs-mon（无 shell、无 home 登录）
#   - 创建目录 /etc/rustfs-mon、/var/lib/rustfs-mon、/var/lib/victoria-metrics
#   - 复制二进制到 /usr/local/bin/rustfs-exporter 和 victoria-metrics
#   - 安装 systemd unit 到 /etc/systemd/system/
#   - 安装 env 文件 /etc/rustfs-mon/exporter.env (权限 0640)
#   - 安装 scrape 配置 /etc/rustfs-mon/victoria-metrics/scrape.yml
#   - 跑 restorecon 标 SELinux 标签
#   - systemctl daemon-reload
#   - systemctl enable --now（自动启动两个服务）
```

### 2.5 验证

```bash
# 7) 服务状态
sudo systemctl status rustfs-exporter --no-pager
sudo systemctl status victoria-metrics --no-pager
# 两个都应该 active (running)

# 8) 端口监听
sudo ss -tlnp | grep -E ':(9090|8429)\b'
# 期望看到 rustfs-exporter 监听 :9090，victoria-metrics 监听 :8429

# 9) exporter 输出真实指标
curl -s localhost:9090/metrics | grep ^rustfs_up
# 期望：rustfs_up 1

# 10) VictoriaMetrics 抓到 exporter 数据
curl -s 'localhost:8429/api/v1/query?query=rustfs_up'
# 期望返回 value=[时间戳, "1"]

# 11) SELinux 没拒绝（仅当 setenforce 1 时有效）
sudo ausearch -m AVC -ts recent 2>&1 | grep -E 'rustfs-exporter|victoria-metrics' | wc -l
# 期望：0
```

### 2.6 SELinux 配置详解

> 默认情况：**什么都没做也能跑**。二进制装在 `/usr/local/bin/`（标准 `bin_t` 标签，unconfined_t 域），数据在 `/var/lib/`（`var_lib_t`），env 在 `/etc/`（`etc_t`）。RHEL targeted policy 默认放行。

但如果要**主动检查或强化**，按下面做：

```bash
# === 检查当前 SELinux 模式 ===
getenforce
# 输出 Enforcing / Permissive / Disabled

# === 查看 binary 的当前 SELinux 标签 ===
ls -lZ /usr/local/bin/rustfs-exporter
# 期望：unconfined_u:object_r:bin_t:s0

# === 如果标签不对（极少见），手动修复 ===
sudo restorecon -v /usr/local/bin/rustfs-exporter
sudo restorecon -Rv /var/lib/rustoria-metrics /var/lib/rustfs-mon

# === 永久关闭 SELinux（不推荐生产）===
sudo sed -i 's/^SELINUX=enforcing/SELINUX=permissive/' /etc/selinux/config
sudo reboot

# === 切换到 permissive（不强制但记录违规）===
sudo setenforce 0
getenforce   # 输出 Permissive

# === 切换回 enforcing ===
sudo setenforce 1
getenforce   # 输出 Enforcing

# === 如果出现 AVC 拒绝（极少见） ===
sudo ausearch -m AVC -ts recent | grep -E 'rustfs-exporter|victoria-metrics'
# 输出形如：... type=AVC msg=... denied { ... } ...
# 解决方法（最简单）：给整个目录打 unconfined 标签
sudo semanage fcontext -a -t bin_t '/usr/local/bin/rustfs-exporter'
sudo restorecon -v /usr/local/bin/rustfs-exporter
```

**SELinux 矩阵测试**（如果想在所有 3 种模式下都验证）：

```bash
sudo systemd/tests/selinux.sh enforcing   # 切到 enforcing，验证服务 + 指标 + 无 AVC
sudo systemd/tests/selinux.sh permissive  # 切到 permissive，验证同上
# 'disabled' 模式需要改 /etc/selinux/config + reboot，单独跑
```

### 2.7 数据存哪里 & 怎么改保留时间

| 路径 | 内容 | 改它做什么 |
|---|---|---|
| `/var/lib/victoria-metrics/` | VM 的所有时序数据 | 删这目录 = 丢全部指标历史 |
| `/var/lib/rustfs-mon/` | exporter 工作目录（几乎为空） | — |
| `/etc/rustfs-mon/exporter.env` | exporter 配置（凭证！） | 改完 `sudo systemctl restart rustfs-exporter` |
| `/etc/rustfs-mon/victoria-metrics/scrape.yml` | VM 抓取列表 | 改完 `sudo systemctl restart victoria-metrics` |
| `/etc/systemd/system/rustfs-exporter.service` | exporter unit | 改完 `sudo systemctl daemon-reload && sudo systemctl restart rustfs-exporter` |
| `/etc/systemd/system/victoria-metrics.service` | VM unit（含 retentionPeriod） | 改完同上 |

**改 VM 数据保留时间（默认 30 天）**：

```bash
# 编辑 unit 文件
sudo vim /etc/systemd/system/victoria-metrics.service
# 找这一行：
#   -retentionPeriod=30d
# 改成你想要的天数：-retentionPeriod=90d
# 重启服务
sudo systemctl daemon-reload
sudo systemctl restart victoria-metrics
# 验证
curl -s 'localhost:8429/api/v1/status' | head -c 200
```

**改 VM 监听端口（默认 8429）**：

```bash
sudo vim /etc/systemd/system/victoria-metrics.service
# 找这一行：
#   -httpListenAddr=:8429
# 改成你想要的：-httpListenAddr=:9001
sudo systemctl daemon-reload
sudo systemctl restart victoria-metrics
```

**改 VM 内存上限（默认 2G）**：

```bash
sudo vim /etc/systemd/system/victoria-metrics.service
# 找 MemoryMax=2G，改成比如 MemoryMax=4G
sudo systemctl daemon-reload
sudo systemctl restart victoria-metrics
```

**VM 数据目录迁移到其他盘**（比如 SSD）：

```bash
# 第 1 步：停服务
sudo systemctl stop victoria-metrics

# 第 2 步：rsync 数据到新盘
sudo rsync -a /var/lib/victoria-metrics/ /mnt/ssd/victoria-metrics/

# 第 3 步：改 unit 文件 -storageDataPath
sudo vim /etc/systemd/system/victoria-metrics.service
# 把 -storageDataPath=/var/lib/victoria-metrics 改成 -storageDataPath=/mnt/ssd/victoria-metrics

# 第 4 步：删除旧目录（确认新盘跑得起来后再删）
sudo systemctl start victoria-metrics
curl -s 'localhost:8429/api/v1/query?query=rustfs_up'   # 验证有数据
sudo rm -rf /var/lib/victoria-metrics
```

### 2.8 防火墙（如果启用 firewalld）

```bash
# 在本主机上
sudo firewall-cmd --permanent --add-port=9090/tcp   # exporter
sudo firewall-cmd --permanent --add-port=8429/tcp   # victoria-metrics
sudo firewall-cmd --reload
sudo firewall-cmd --list-ports
# 应输出：9090/tcp 8429/tcp

# 如果 VM 需要被远程 OpenShift 集群抓取
sudo firewall-cmd --permanent --add-rich-rule='
  rule family=ipv4 source address=<OPENSHIFT_EGRESS_CIDR>
  port port=8429 protocol=tcp accept'
sudo firewall-cmd --reload
```

### 2.9 升级

```bash
# 升级 exporter
cd deploy/monitoring
git pull
cd exporter && CGO_ENABLED=0 go build ... -o ../systemd/rustfs-exporter ./cmd/exporter && cd ..
sudo cp systemd/rustfs-exporter /usr/local/bin/rustfs-exporter
sudo systemctl restart rustfs-exporter

# 升级 VictoriaMetrics
# 改 systemd/install.sh 里的 VM_VERSION，然后重跑 install
sudo systemd/install.sh
# 或手动：
curl -fsSL -o /tmp/vm-new "https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/v1.150.0/victoria-metrics-linux-amd64-v1.150.0"
chmod +x /tmp/vm-new
sudo systemctl stop victoria-metrics
sudo install -m 0755 /tmp/vm-new /usr/local/bin/victoria-metrics
sudo systemctl start victoria-metrics
```

### 2.10 卸载

```bash
# 保留配置和数据
sudo systemd/uninstall.sh

# 完全删除（包括 /var/lib/victoria-metrics 里的所有历史数据！）
sudo systemd/uninstall.sh --purge-data
```

---

## 3. OpenShift Grafana（远端集群）

**适用**：你的 OpenShift 4.x 集群在外网/VPN 内，VictoriaMetrics 跑在另外的主机上（docker compose 或 systemd 部署），Grafana 部署到集群。

**架构**：

```
   OpenShift 4.x 集群                           你的主机 (VM 所在)
┌─────────────────────────┐                  ┌──────────────────┐
│ Deployment: grafana     │                  │  VictoriaMetrics  │
│   └─ Container          │   PromQL/HTTP    │    :8429          │
│       (13.0.2)          │ ────────────────▶│                   │
│   ↓                     │   (集群 egress   └──────────────────┘
│ Service (ClusterIP)     │    必须能到达)
│   ↓                     │
│ Route (TLS edge)        │
│   ↓                     │
│ https://grafana-        │
│  rustfs-monitoring.     │
│  apps.cluster.local     │
└─────────────────────────┘
```

### 3.1 前置要求

| 工具 | 用途 | 检查 |
|---|---|---|
| `oc` CLI 4.11+ | 部署工具 | `oc version` |
| 集群 cluster-admin 权限（或受限的 namespace 创建权限） | 至少能 `oc new-project` + `oc apply` | `oc whoami --show-server` |
| 主机能 SSH 进 VM 所在主机 | 配防火墙规则 | — |

### 3.2 部署步骤

```bash
# 第 1 步：进入项目目录
cd deploy/monitoring/openshift

# 第 2 步：登录 OpenShift
oc login https://api.cluster.example.com:6443 --token=sha256~xxxx
oc whoami   # 应显示你的用户名

# 第 3 步：创建项目（一次性）
oc new-project rustfs-monitoring
# 输出：Now using project "rustfs-monitoring" on server ...

# 第 4 步：生成 Grafana admin 密码
GRAFANA_PASS=$(openssl rand -base64 24 | tr -d '\n=' | head -c 32)
echo "Grafana admin password: $GRAFANA_PASS"
# ⚠️ 保存这个密码！

# 第 5 步：把密码写进 kustomization.yaml
sed -i "s/REPLACE_WITH_RANDOM_STRING/$GRAFANA_PASS/" kustomization.yaml
grep password kustomization.yaml   # 应看到你的密码（base64 之前的明文，kustomize 会处理）

# 第 6 步：填 VM 远程地址
#    把 config-datasource.yaml 里的 ${VM_REMOTE_URL} 替换成实际地址
sed -i "s|\${VM_REMOTE_URL}|http://vm-host.example.com:8429|" config-datasource.yaml
grep "url:" config-datasource.yaml
# 期望：url: http://vm-host.example.com:8429

# 第 7 步：应用所有资源
oc apply -k .
# 输出：
#   namespace/rustfs-monitoring (configured / unchanged)
#   serviceaccount/grafana created
#   deployment.apps/grafana created
#   service/grafana created
#   route.route.openshift.io/grafana created
#   configmap/grafana-datasources created
#   configmap/grafana-dashboards-cfg created
#   configmap/grafana-dashboard-rustfs created
#   configmap/grafana-alerts created
#   secret/grafana-admin created

# 第 8 步：等 Grafana pod 启动
oc rollout status deploy/grafana -n rustfs-monitoring
# 输出：deployment "grafana" successfully rolled out
```

### 3.3 网络要求（重要！）

**OpenShift 集群的 egress 必须能到达 VM 的 8429 端口。**

```bash
# 在 VM 所在的主机上（firewalld）：
sudo firewall-cmd --permanent --add-rich-rule='
  rule family=ipv4
  source address=<集群 egress CIDR>
  port port=8429 protocol=tcp accept'
sudo firewall-cmd --reload

# 怎么知道集群的 egress CIDR？
# 方法 1：问集群管理员
# 方法 2：从 OpenShift 内部 netcheck：
oc run netcheck --image=registry.access.redhat.com/ubi9/ubi-minimal \
  --rm -it --restart=Never -- curl -v http://你的VM:8429/health
# 看 output 里的 source IP，那就是出口 IP
```

### 3.4 验证

```bash
# 9) Pod 健康
oc get pods -n rustfs-monitoring
# 应：grafana-xxx-yyy  1/1  Running

# 10) Grafana API 健康
oc rsh -n rustfs-monitoring deploy/grafana \
  curl -s http://localhost:3000/api/health
# 期望：{"version":"13.0.2","database":"ok"}

# 11) Datasource 健康（最关键 — 证明 VM 通）
oc rsh -n rustfs-monitoring deploy/grafana \
  curl -s -u admin:$GRAFANA_PASS \
    http://localhost:3000/api/datasources/uid/PBFA97CFB590B2093/health
# 期望：{"message":"Successfully queried the Prometheus API","status":"OK"}

# 12) 获取 Route URL（外网访问）
ROUTE=$(oc get route grafana -n rustfs-monitoring \
  -o jsonpath='{.spec.host}')
echo "Grafana URL: https://$ROUTE"

# 13) 浏览器打开（用 $GRAFANA_PASS 登录）
xdg-open "https://$ROUTE"  # 或手动复制
```

### 3.5 资源说明

| 资源 | 文件 | 改什么 |
|---|---|---|
| `Namespace/rustfs-monitoring` | `namespace.yaml` | 名字 |
| `ServiceAccount/grafana` | `grafana-deployment.yaml` | 不需要改 |
| `Deployment/grafana` | `grafana-deployment.yaml` | `image`、`resources`、`replicas` |
| `Service/grafana` | `grafana-service.yaml` | `port` |
| `Route/grafana` | `grafana-route.yaml` | `host`、`tls.certificate`（自定义证书） |
| `ConfigMap/grafana-datasources` | `config-datasource.yaml` | **VM URL（必须改）** |
| `ConfigMap/grafana-dashboards-cfg` | `config-dashboards-cfg.yaml` | `updateIntervalSeconds` |
| `ConfigMap/grafana-dashboard-rustfs` | `dashboards-rustfs.yaml` | 嵌入的 dashboard JSON |
| `ConfigMap/grafana-alerts` | `config-alerts.yaml` | 报警阈值 |
| `Secret/grafana-admin` | `kustomization.yaml` | **密码（必须改）** |

### 3.6 Grafana 数据存哪里？

Grafana 13.0.2 在这个部署里用的是 `emptyDir`（pod 重启数据丢失），但：
- **仪表板**：从 ConfigMap 挂载，pod 重启自动恢复
- **datasource / 报警规则**：同上
- **用户登录会话 / 报警静音 / 注释**：存在 sqlite3 数据库（在 emptyDir 里）→ **pod 重启会丢**

**如果要保留用户数据**：改 `grafana-deployment.yaml` 里 `volumes` 部分，用 PVC 替换 `emptyDir`：

```yaml
volumes:
  - name: storage
    persistentVolumeClaim:
      claimName: grafana-data   # 提前 oc apply 一个 PVC
```

### 3.7 升级

```bash
# 改 Grafana 版本
sed -i 's/grafana:13.0.2/grafana:13.1.0/' openshift/grafana-deployment.yaml
oc rollout restart deploy/grafana -n rustfs-monitoring
oc rollout status deploy/grafana -n rustfs-monitoring
```

### 3.8 卸载

```bash
oc delete -k openshift/
oc delete project rustfs-monitoring   # 顺手删整个 namespace
```

---

## 4. 跨模式通用操作

### 4.1 配置多个 rustfs 实例

**适用场景**：源端和目标端是两个独立 rustfs，但想在一个 Grafana 仪表板里同时看。

用 `RUSTFS_TARGETS_JSON` 一次配置多个目标。所有指标自动带 `cluster=<name>` 标签，dashboard 顶部有 `cluster` 模板变量用来筛选。

#### Compose 模式（`.env`）

```bash
vim .env
# 把 RUSTFS_ENDPOINT/ACCESS_KEY/SECRET_KEY 这些注释掉，改用：
RUSTFS_TARGETS_JSON='[
  {
    "name": "source",
    "endpoint": "https://10.0.50.15:9000",
    "access_key": "admin",
    "secret_key": "VMware1!",
    "tls_skip_verify": true
  },
  {
    "name": "target",
    "endpoint": "https://10.0.50.18:9000",
    "access_key": "rustfsadmin",
    "secret_key": "rustfsadmin"
  }
]'

docker compose restart exporter
```

#### systemd 模式（`/etc/rustfs-mon/exporter.env`）

```bash
sudo systemctl stop rustfs-exporter
sudo vim /etc/rustfs-mon/exporter.env
# 在文件里加（或修改）：
RUSTFS_TARGETS_JSON='[
  {"name":"source","endpoint":"https://10.0.50.15:9000","access_key":"admin","secret_key":"VMware1!","tls_skip_verify":true},
  {"name":"target","endpoint":"https://10.0.50.18:9000","access_key":"rustfsadmin","secret_key":"rustfsadmin"}
]'
sudo systemctl start rustfs-exporter
sudo journalctl -u rustfs-exporter -n 5
# 应输出：
#   loaded 2 rustfs target(s)
#     - source @ https://10.0.50.15:9000
#     - target @ https://10.0.50.18:9000
```

#### OpenShift 模式

OpenShift 模式下 exporter 不在集群里跑（你自己用 compose/systemd 跑），所以改的是**那台跑 exporter 的主机**的 env。OpenShift 上的 Grafana 不需要改——它从 VM 抓的所有 `cluster=` 标签会自动出现。

#### 验证

```bash
# 看 /metrics 里两个 cluster 都出现
curl -s localhost:9090/metrics | grep '^rustfs_health_ready' | awk -F'cluster="' '{print $2}' | awk -F'"' '{print "  cluster="$1}' | sort -u
# 应输出：
#   cluster=source
#   cluster=target

# VM 端查询特定 cluster
curl -s 'http://localhost:8429/api/v1/query?query=rustfs_health_ready{cluster="source"}'
```

#### Dashboard 用法

打开 Grafana "RustFS" 仪表板，顶部有两个下拉框：
- **cluster** — 默认 "All"，可单选或多选特定 rustfs 实例
- **bucket** — 默认 "All"，可按桶筛选

可以组合筛，例如只看 source 集群的 rustfs15 桶：`cluster=source, bucket=rustfs15`。

### 4.2 升级 rustfs 凭证（轮换密钥）

```bash
# Compose
vim deploy/monitoring/.env
docker compose restart exporter

# systemd
sudo vim /etc/rustfs-mon/exporter.env
sudo systemctl restart rustfs-exporter

# OpenShift（VM 凭证不变，OpenShift 只连 VM，VM 自己连 rustfs）
# 不需要在这边操作
```

### 4.2 修改报警阈值

报警规则在 `conf/grafana-alerts.yaml`，比如 "Replication pending too large" 默认 10GB 触发：

```yaml
expr: rustfs_replication_pending_bytes > 10e9   # 改成 > 50e9 即 50GB
```

| 模式 | 怎么生效 |
|---|---|
| Compose | `docker compose restart grafana`（Grafana 每 30s 重读配置文件） |
| systemd | 不适用（OpenShift 模式才有 alerts） |
| OpenShift | `oc apply -k openshift/`（Grafana 自动 reload） |

### 4.3 防火墙端口速查

| 模式 | 端口 | 服务 |
|---|---|---|
| Compose | 9090 | exporter |
| Compose | 8429 | VictoriaMetrics |
| Compose | 3000 | Grafana |
| systemd | 9090 | exporter |
| systemd | 8429 | VictoriaMetrics |
| OpenShift | 443 (Route) | Grafana HTTPS（OpenShift router 端口） |
| OpenShift | 8429 (VM 主机) | VictoriaMetrics |

### 4.4 查看日志

```bash
# Compose
docker compose logs -f exporter
docker compose logs -f victoria-metrics

# systemd
sudo journalctl -u rustfs-exporter -f
sudo journalctl -u victoria-metrics -f

# OpenShift
oc logs -n rustfs-monitoring -l app=grafana -f
```