# OpenShift Grafana deployment

把 Grafana 13.0.2 部署到 OpenShift 4.x 集群，抓取**远程** VictoriaMetrics（VM 跑在另外的主机，用 compose 或 systemd 部署）。

完整文档在 [`../DEPLOY.md §3`](../DEPLOY.md#3-openshift-grafana-远端集群)。本文档只列核心步骤。

## 架构

```
OpenShift 集群                              远程主机
┌────────────────────────────┐            ┌──────────────┐
│ Deployment: grafana-13.0.2 │            │  VictoriaMtrcs│
│   ↓                        │  PromQL    │  :8429       │
│ Service (ClusterIP :3000)  │ ──────────▶│              │
│   ↓                        │            └──────────────┘
│ Route (TLS edge :443)      │
│   ↓                        │
│ https://grafana-           │
│  rustfs-monitoring.apps.   │
│  <cluster-domain>          │
└────────────────────────────┘
```

**前提**：OpenShift 集群的 egress 网络能到达远程 VM 的 8429 端口。

## 快速部署（5 分钟）

```bash
cd deploy/monitoring/openshift

# 1. 登录集群
oc login https://api.cluster.example.com:6443 --token=...

# 2. 创建项目
oc new-project rustfs-monitoring

# 3. 生成 Grafana 密码 + 填进 kustomization.yaml
GRAFANA_PASS=$(openssl rand -base64 24 | tr -d '\n=' | head -c 32)
echo "Grafana admin password: $GRAFANA_PASS"   # ⚠️ 保存
sed -i "s/REPLACE_WITH_RANDOM_STRING/$GRAFANA_PASS/" kustomization.yaml

# 4. 填 VM 远程 URL
sed -i "s|\${VM_REMOTE_URL}|http://vm-host.example.com:8429|" config-datasource.yaml

# 5. 应用全部资源
oc apply -k .

# 6. 等 pod ready
oc rollout status deploy/grafana -n rustfs-monitoring

# 7. 验证 datasource 能连 VM
oc rsh -n rustfs-monitoring deploy/grafana \
  curl -s -u admin:$GRAFANA_PASS \
    http://localhost:3000/api/datasources/uid/PBFA97CFB590B2093/health
# 期望: "Successfully queried the Prometheus API"

# 8. 拿 Route URL
ROUTE=$(oc get route grafana -n rustfs-monitoring -o jsonpath='{.spec.host}')
echo "Open: https://$ROUTE"
```

## 网络要求

**集群 egress 必须能到达 VM 主机:8429。**

```bash
# 在 VM 主机上允许集群 IP 段
sudo firewall-cmd --permanent --add-rich-rule='
  rule family=ipv4 source address=<集群 egress CIDR>
  port port=8429 protocol=tcp accept'
sudo firewall-cmd --reload

# 验证网络（从集群内 pod 出发）
oc run netcheck --image=registry.access.redhat.com/ubi9/ubi-minimal \
  --rm -it --restart=Never -- curl -v http://vm-host.example.com:8429/health
```

## 资源清单

`oc get all,configmap,route,secret -n rustfs-monitoring` 应显示：

| 资源 | 文件 | 必改项 |
|---|---|---|
| Namespace `rustfs-monitoring` | `namespace.yaml` | 名字 |
| ServiceAccount `grafana` | `grafana-deployment.yaml` | — |
| Deployment `grafana` | `grafana-deployment.yaml` | image 版本、resources、replicas |
| Service `grafana` | `grafana-service.yaml` | port |
| Route `grafana` | `grafana-route.yaml` | 自定义 host、TLS 证书 |
| ConfigMap `grafana-datasources` | `config-datasource.yaml` | **VM URL** |
| ConfigMap `grafana-dashboards-cfg` | `config-dashboards-cfg.yaml` | updateIntervalSeconds |
| ConfigMap `grafana-dashboard-rustfs` | `dashboards-rustfs.yaml` | 嵌入 dashboard JSON |
| ConfigMap `grafana-alerts` | `config-alerts.yaml` | 报警阈值 |
| Secret `grafana-admin` | `kustomization.yaml` | **密码** |

## 故障排查

### Grafana 起来了但 datasource health 报错

```bash
oc rsh -n rustfs-monitoring deploy/grafana \
  curl -v -u admin:$GRAFANA_PASS \
    http://localhost:3000/api/datasources/uid/PBFA97CFB590B2093/health
# 看完整错误信息
```

最常见原因：
1. **网络不通** — 检查 egress 防火墙（见上）
2. **VM URL 填错** — `oc edit cm grafana-datasources -n rustfs-monitoring`
3. **VM 有 HTTP basic auth 但 datasource 没配** — 见 VM auth 章节

### Pod 起不来 / ImagePullBackOff

```bash
oc describe pod -n rustfs-monitoring -l app=grafana
# 看 Events 段最后几行
```

最常见：
- 集群无 internet 访问 ghcr.io → 把 `grafana/grafana:13.0.2` 镜像同步到内部 registry，改 deployment `image`
- 内部 registry 需要 pull secret → 加 `imagePullSecrets`

### Route 返回 503 / 504

```bash
oc get pods -n rustfs-monitoring    # pod running?
oc get endpoints grafana -n rustfs-monitoring   # service 后端有 IP?
oc logs -n rustfs-monitoring -l app=grafana --tail=50
```

### 仪表板不显示

```bash
oc rsh -n rustfs-monitoring deploy/grafana \
  ls /var/lib/grafana/dashboards/
# 应输出：rustfs.json

# 检查 provisioning
oc rsh -n rustfs-monitoring deploy/grafana \
  ls /etc/grafana/provisioning/dashboards/
# 应输出：dashboards.yaml
```

### 报警规则不显示

```bash
oc rsh -n rustfs-monitoring deploy/grafana \
  ls /etc/grafana/provisioning/alerting/
# 应输出：rules.yaml
```

## 给 VM 加 HTTP basic auth

VM 默认无 auth。如果想让 Grafana 用密码连 VM：

**第 1 步**：VM 主机上生成密码文件：
```bash
sudo mkdir -p /etc/rustfs-mon
echo -n "myStrongPassword123" | sudo tee /etc/rustfs-mon/vm.password
sudo chown rustfs-mon:rustfs-mon /etc/rustfs-mon/vm.password
sudo chmod 0400 /etc/rustfs-mon/vm.password
```

**第 2 步**：改 VM unit：
```bash
sudo systemctl edit victoria-metrics   # 加 override
# 内容：
#   [Service]
#   ExecStart=
#   ExecStart=/usr/local/bin/victoria-metrics \
#       -storageDataPath=/var/lib/victoria-metrics \
#       -retentionPeriod=30d \
#       -promscrape.config=/etc/rustfs-mon/victoria-metrics/scrape.yml \
#       -memory.allowedPercent=40 \
#       -httpListenAddr=:8429 \
#       -httpAuth.username=monitoring \
#       -httpAuth.passwordFile=/etc/rustfs-mon/vm.password
sudo systemctl daemon-reload
sudo systemctl restart victoria-metrics
curl -u monitoring:myStrongPassword123 -s localhost:8429/health
# 应返回：OK
```

**第 3 步**：Grafana datasource 加 basic auth：

改 `openshift/config-datasource.yaml`：
```yaml
datasources.yaml: |
  apiVersion: 1
  datasources:
    - name: VictoriaMetrics
      uid: PBFA97CFB590B2093
      type: prometheus
      access: proxy
      url: http://vm-host.example.com:8429
      basicAuth: true
      basicAuthUser: monitoring
      secureJsonData:
        basicAuthPassword: myStrongPassword123   # 不安全！下面用 Secret
      isDefault: true
      editable: false
```

**生产推荐**：用 Kubernetes Secret 存密码，然后通过 env 注入 datasource：

```bash
oc create secret generic vm-auth -n rustfs-monitoring \
  --from-literal=password=myStrongPassword123
```

然后改 datasource ConfigMap 用 `env.VM_PASSWORD` 引用（kustomize 支持）。

## Grafana 数据持久化（生产推荐）

默认 `emptyDir`，pod 重启丢用户会话/报警静音。改成 PVC：

```yaml
# grafana-deployment.yaml 里把
volumes:
  - name: storage
    emptyDir: {}
# 改成
volumes:
  - name: storage
    persistentVolumeClaim:
      claimName: grafana-data
```

提前创建 PVC：
```bash
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
```

## 升级

```bash
# 改 Grafana 版本
sed -i 's/grafana:13.0.2/grafana:13.1.0/' openshift/grafana-deployment.yaml
oc rollout restart deploy/grafana -n rustfs-monitoring
oc rollout status deploy/grafana -n rustfs-monitoring
```

## 卸载

```bash
oc delete -k openshift/
oc delete project rustfs-monitoring
```