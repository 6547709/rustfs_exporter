# Storage & Retention Configuration

本文档说明 rustfs_exporter 监控栈在三种部署模式下，数据存在哪里、保留多久、怎么改、怎么备份迁移。

## 0. 速查表

| 模式 | VM 数据目录 | Grafana 数据 | 默认保留时间 | 改保留时间 |
|---|---|---|---|---|
| docker compose | `monitoring_vm-data` volume | `monitoring_grafana-data` volume | **30 天** | `docker-compose.yml` 改 `-retentionPeriod` |
| systemd | `/var/lib/victoria-metrics/` | （不在栈内） | **30 天** | unit 文件改 `-retentionPeriod=` |
| OpenShift | （远程，不在 OpenShift 上） | `emptyDir`（pod 重启丢） | 30 天 | 远程 VM 上改 |

## 1. VictoriaMetrics 数据

### 1.1 数据存什么

VM 把所有 scrape 来的时序数据存在一个目录里，包括：
- 所有 `rustfs_*` 指标（每桶 + 每组件 + 健康 + 复制）
- 索引文件（标签 → 时间序列映射）
- 合并 / 去重后的块文件

**该目录有多大？** 粗略估算：
- 13 个指标 × 1 个桶 = 13 条时间序列
- 30 秒采样 × 30 天 = 86,400 个数据点/序列
- 每个数据点 ~32 bytes
- 总计：~36 MB

实际加上索引、压缩，大约 **50-100 MB / 月 / 桶**。10 个桶约 1 GB/月。

### 1.2 docker compose 模式

**数据位置**：
```bash
# 查看实际路径
docker volume inspect monitoring_vm-data
# 输出：... "Mountpoint": "/var/lib/docker/volumes/monitoring_vm-data/_data"
```

**改保留时间**：

```bash
# 编辑 compose 文件
vim deploy/monitoring/docker-compose.yml
# vm 服务下找：
#   - "-retentionPeriod=30d"
# 改成：- "-retentionPeriod=90d"   (90 天)

# 重启 VM 服务
docker compose up -d vm

# 验证（要等几秒）
docker compose exec vm ls -la /storage   # 看 data 目录
```

**迁移到其他盘**（比如数据盘快满了）：

```bash
# 第 1 步：停 VM
docker compose stop vm

# 第 2 步：rsync 数据
sudo rsync -a /var/lib/docker/volumes/monitoring_vm-data/_data/ \
            /mnt/new-disk/vm-data/

# 第 3 步：编辑 compose 把 volume 改 bind mount
# 把
#   volumes:
#     - vm-data:/storage
# 改成
#   volumes:
#     - /mnt/new-disk/vm-data:/storage

# 第 4 步：删旧 volume（可选）
docker compose up -d vm
curl -s 'localhost:8429/api/v1/query?query=rustfs_up'   # 验证还有数据
docker volume rm monitoring_vm-data
```

**完全清理**（数据 + 配置 + 容器）：
```bash
docker compose down -v   # -v 删 volumes
```

### 1.3 systemd 模式

**数据位置**：`/var/lib/victoria-metrics/`（owner: `rustfs-mon:rustfs-mon`，mode 0750）

**改保留时间**：

```bash
sudo vim /etc/systemd/system/victoria-metrics.service
# 找这行（在 ExecStart= 内）：
#   -retentionPeriod=30d
# 改成  -retentionPeriod=90d

sudo systemctl daemon-reload
sudo systemctl restart victoria-metrics

# 验证
sudo journalctl -u victoria-metrics --no-pager -n 5
# 应看到启动日志包含 "-retentionPeriod=90d"
```

**改数据目录**：

```bash
# 第 1 步：停 VM
sudo systemctl stop victoria-metrics

# 第 2 步：rsync 到新位置
sudo rsync -a /var/lib/victoria-metrics/ /mnt/ssd/victoria-metrics/
sudo chown -R rustfs-mon:rustfs-mon /mnt/ssd/victoria-metrics

# 第 3 步：改 unit 文件
sudo vim /etc/systemd/system/victoria-metrics.service
# 找：-storageDataPath=/var/lib/victoria-metrics
# 改：-storageDataPath=/mnt/ssd/victoria-metrics

sudo systemctl daemon-reload
sudo systemctl start victoria-metrics
curl -s 'localhost:8429/api/v1/query?query=rustfs_up'   # 验证

# 第 4 步（确认 OK 后）：删旧目录
sudo rm -rf /var/lib/victoria-metrics
```

**改内存上限**：

```bash
sudo vim /etc/systemd/system/victoria-metrics.service
# 找：MemoryMax=2G
# 改：MemoryMax=4G
sudo systemctl daemon-reload
sudo systemctl restart victoria-metrics
```

> VM 内部 `-memory.allowedPercent=40` 是 VM 自己用的内存占比（占 `MemoryMax` 的 40%）。改 `MemoryMax=4G` 后 VM 会用 ~1.6G 缓存，剩下的留给系统。

**完全清理**：
```bash
sudo systemd/uninstall.sh --purge-data   # 删 /var/lib/victoria-metrics + 用户
```

### 1.4 OpenShift 模式

VM 不在 OpenShift 上，**改远程 VM 上的设置**（参见 §1.2 或 §1.3）。

## 2. Grafana 数据

### 2.1 存什么

| 数据 | 是否重要 | 在哪 |
|---|---|---|
| **仪表板 JSON** | 不重要 | ConfigMap 挂载的，pod 重启自动恢复 |
| **报警规则** | 不重要 | 同上 |
| **datasource 配置** | 不重要 | 同上 |
| **用户账号 / 密码** | 看情况 | 来自 Secret，跟 ConfigMap 一起 |
| **用户登录会话** | 重要 | `emptyDir`，**pod 重启丢** |
| **报警静音** | 重要 | 同上 |
| **用户注释** | 重要 | 同上 |
| **临时查询历史** | 不重要 | `emptyDir`，丢就丢 |

### 2.2 docker compose 模式

数据 volume `monitoring_grafana-data`，挂到 `/var/lib/grafana`。

**改 admin 密码**：
```bash
vim .env
# 改 GF_ADMIN_PASS=新密码
docker compose up -d grafana
```

**完全清理**：
```bash
docker compose down -v
```

### 2.3 systemd 模式

**Grafana 不在 systemd 栈里**。如果要跑 Grafana 在 systemd 主机上：

```bash
# 用容器跑（最简单）
docker run -d --name grafana \
  --restart unless-stopped \
  -p 3000:3000 \
  -v /var/lib/grafana:/var/lib/grafana \
  -e GF_SECURITY_ADMIN_PASSWORD=你的密码 \
  grafana/grafana:13.0.2

# 或用官方 RPM 包（RHEL）
sudo tee /etc/yum.repos.d/grafana.repo <<'EOF'
[grafana]
name=grafana
baseurl=https://rpm.grafana.com
repo_gpgcheck=1
enabled=1
gpgcheck=1
gpgkey=https://rpm.grafana.com/gpg.key
EOF
sudo dnf install -y grafana
sudo systemctl enable --now grafana-server
```

### 2.4 OpenShift 模式

**默认**：emptyDir → pod 重启丢用户数据。

**生产推荐**：改 PVC（5 GB 起步）：

```bash
# 1. 创建 PVC
oc apply -f - <<'EOF'
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: grafana-data
  namespace: rustfs-monitoring
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 5Gi
EOF

# 2. 改 grafana-deployment.yaml
# volumes:
#   - name: storage
#     emptyDir: {}
# 改成
#   - name: storage
#     persistentVolumeClaim:
#       claimName: grafana-data

# 3. 重启
oc apply -f openshift/grafana-deployment.yaml
oc rollout restart deploy/grafana -n rustfs-monitoring
```

## 3. exporter 数据

**Exporter 是无状态的**。它每次启动就重新从 rustfs 拉数据，不存任何东西。

不需要备份/迁移。

## 4. 备份策略建议

| 数据 | 备份频率 | 保留 | 方法 |
|---|---|---|---|
| VM 数据 | 每天 | 7 天 | `rsync` 到备份盘（详见 §1.3） |
| exporter 配置 | 每次改 | 永久 | `/etc/rustfs-mon/exporter.env` 入 git |
| Grafana 仪表板 | 改时 | 永久 | `dashboards/rustfs.json` 入 git |
| 报警规则 | 改时 | 永久 | `conf/grafana-alerts.yaml` 入 git |
| Grafana 用户数据 | 看业务 | 看业务 | PVC snapshot（OpenShift）/volume backup（compose） |

## 5. 容量规划公式

**VM 数据大小** ≈ 指标数 × 桶数 × 30 × 86400 × 32 bytes × 保留天数 / 30

示例：
- 13 个指标 × 5 个桶 × 30 天 = ~50 MB
- 13 × 5 × 90 天 = ~150 MB
- 13 × 50 × 365 天 = ~7 GB

预留 **2-3x** 缓冲用于索引 + 压缩比变化。