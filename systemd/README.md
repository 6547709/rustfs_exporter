# systemd deployment

把 `rustfs-exporter` 和 `victoria-metrics` 装成 Linux 系统服务（systemd）。
适用于 RHEL 9 / Rocky Linux 9 / AlmaLinux 9 / Ubuntu 22.04+。

完整文档在 [`../DEPLOY.md §2`](../DEPLOY.md#2-systemd)。本文档只列核心命令速查。

## 快速安装（5 分钟）

```bash
cd deploy/monitoring

# 1. 构建 exporter 二进制
(cd exporter && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
  -o ../systemd/rustfs-exporter ./cmd/exporter)

# 2. 下载 VictoriaMetrics（amd64 或 arm64）
VM_VER=v1.146.0
curl -fsSL -o systemd/victoria-metrics \
  "https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/${VM_VER}/victoria-metrics-linux-amd64-${VM_VER}"
chmod +x systemd/victoria-metrics

# 3. 填配置
cp systemd/env/rustfs-exporter.env.example /tmp/exporter.env
$EDITOR /tmp/exporter.env   # 改 RUSTFS_ENDPOINT / ACCESS_KEY / SECRET_KEY

# 4. 安装
sudo EXPORTER_BIN="$PWD/systemd/rustfs-exporter" \
     EXPORTER_ENV=/tmp/exporter.env \
     systemd/install.sh

# 5. 验证
sudo systemctl status rustfs-exporter victoria-metrics
curl -s localhost:9090/metrics | grep ^rustfs_up
curl -s 'localhost:8429/api/v1/query?query=rustfs_up'
```

## 安装后改配置

| 想改 | 改哪个文件 | 重启命令 |
|---|---|---|
| rustfs 凭证 | `/etc/rustfs-mon/exporter.env` | `sudo systemctl restart rustfs-exporter` |
| scrape 间隔 | `/etc/rustfs-mon/victoria-metrics/scrape.yml` | `sudo systemctl restart victoria-metrics` |
| VM 数据保留时间 | `/etc/systemd/system/victoria-metrics.service`（找 `-retentionPeriod=30d`） | `sudo systemctl daemon-reload && sudo systemctl restart victoria-metrics` |
| VM 监听端口 | 同上（找 `-httpListenAddr=:8429`） | 同上 |
| VM 内存上限 | 同上（找 `MemoryMax=2G`） | 同上 |
| VM 数据目录 | 同上（找 `-storageDataPath=/var/lib/victoria-metrics`） | 同上 + rsync 数据 |

## SELinux 速查

```bash
# 当前模式
getenforce
# Enforcing / Permissive / Disabled

# 二进制 SELinux 标签（应该自动正确）
ls -lZ /usr/local/bin/rustfs-exporter
# unconfined_u:object_r:bin_t:s0

# 临时切到 permissive（不强制但记录违规）
sudo setenforce 0

# 切回 enforcing
sudo setenforce 1

# 查看最近被 SELinux 拒绝的操作
sudo ausearch -m AVC -ts recent | grep -E 'rustfs|victoria'

# 永久改模式（需要 reboot）
sudo sed -i 's/^SELINUX=.*/SELINUX=permissive/' /etc/selinux/config
sudo reboot
```

完整 SELinux 三态自动测试：

```bash
sudo systemd/tests/selinux.sh enforcing
sudo systemd/tests/selinux.sh permissive
# disabled 模式需要改 /etc/selinux/config + reboot
```

## 数据和日志位置

| 路径 | 内容 |
|---|---|
| `/var/lib/victoria-metrics/` | VM 全部时序数据（删它 = 丢历史） |
| `/var/lib/rustfs-mon/` | exporter 工作目录 |
| `/etc/rustfs-mon/exporter.env` | exporter 配置（含 rustfs 凭证） |
| `/etc/rustfs-mon/victoria-metrics.env` | VM 可选配置 |
| `/etc/rustfs-mon/victoria-metrics/scrape.yml` | VM scrape 列表 |
| `/etc/systemd/system/{rustfs-exporter,victoria-metrics}.service` | unit 文件 |
| `/usr/local/bin/{rustfs-exporter,victoria-metrics}` | 二进制 |
| 日志 | `sudo journalctl -u rustfs-exporter -f` / `-u victoria-metrics -f` |

## 防火墙（firewalld）

```bash
sudo firewall-cmd --permanent --add-port=9090/tcp
sudo firewall-cmd --permanent --add-port=8429/tcp
sudo firewall-cmd --reload
```

只允许特定 IP 访问 VM（推荐远程 OpenShift 抓取时）：

```bash
sudo firewall-cmd --permanent --add-rich-rule='
  rule family=ipv4 source address=<CIDR>
  port port=8429 protocol=tcp accept'
sudo firewall-cmd --reload
```

## 升级

```bash
# 升级 exporter
cd deploy/monitoring
(cd exporter && CGO_ENABLED=0 go build ... -o ../systemd/rustfs-exporter ./cmd/exporter)
sudo install -m 0755 systemd/rustfs-exporter /usr/local/bin/rustfs-exporter
sudo systemctl restart rustfs-exporter

# 升级 VictoriaMetrics
curl -fsSL -o /tmp/vm-new \
  https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/v1.150.0/victoria-metrics-linux-amd64-v1.150.0
chmod +x /tmp/vm-new
sudo systemctl stop victoria-metrics
sudo install -m 0755 /tmp/vm-new /usr/local/bin/victoria-metrics
sudo systemctl start victoria-metrics
```

## 卸载

```bash
# 保留配置和数据
sudo systemd/uninstall.sh

# 完全删除（包括 /var/lib/victoria-metrics 历史）
sudo systemd/uninstall.sh --purge-data
```

## 安装脚本做了什么（`install.sh`）

1. 创建系统用户 `rustfs-mon`（`/sbin/nologin`，无 home 登录）
2. 创建目录 `/etc/rustfs-mon`、`/var/lib/rustfs-mon`、`/var/lib/victoria-metrics`
3. 跑 `restorecon` 标 SELinux 标签（标准路径有默认 policy）
4. 复制二进制到 `/usr/local/bin/`
5. 安装 unit 文件到 `/etc/systemd/system/`
6. 首次启动填入 env 模板到 `/etc/rustfs-mon/`，权限 0640
7. 填入 scrape 配置模板
8. `systemctl daemon-reload && systemctl enable --now`

## systemd unit 硬化（已应用）

- `NoNewPrivileges`、`ProtectSystem=strict`、`ProtectHome`
- `PrivateTmp`、`PrivateDevices`、`ProtectClock`、`ProtectKernelTunables`
- `RestrictSUIDSGID`、`RestrictNamespaces`、`RestrictRealtime`
- `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`
- `LockPersonality`、`MemoryDenyWriteExecute`
- `CapabilityBoundingSet=`（空）

VM 额外：`ReadWritePaths=/var/lib/victoria-metrics`（数据目录）