# OpenShift deployment — Grafana

Run Grafana on a remote **OpenShift 4.x** cluster, scraping the
VictoriaMetrics service that lives on a different host (deployed via
`../systemd/` or `../docker-compose.yml`).

Grafana 13.0.2 + datasource + dashboards + alerts are all provisioned via
ConfigMaps. The datasource URL is parameterized so you can repoint at any
remote VM.

## Network prerequisites

The OpenShift cluster's egress must be allowed to reach the VM host on port
8429 (or whichever port VM listens on). For example, on the VM host's
firewall:

```bash
firewall-cmd --zone=public --add-rich-rule='
  rule family=ipv4 source address=<OPENSHIFT_EGRESS_CIDR> \
  port port=8429 protocol=tcp accept'
```

Get the cluster's egress CIDR:

```bash
oc get egressingresses.network.operator.openshift.io cluster -o jsonpath='{.status.filter[0].cidr}'
# or just allow 0.0.0.0/0 for dev (NOT recommended for prod)
```

## One-time setup

```bash
# 1. Create the project (or use your own namespace)
oc new-project rustfs-monitoring

# 2. Generate a strong Grafana admin password
GRAFANA_PASS=$(openssl rand -base64 24 | tr -d '\n=' | head -c 32)
echo "Grafana admin password: $GRAFANA_PASS"
# Edit kustomization.yaml and replace REPLACE_WITH_RANDOM_STRING with $GRAFANA_PASS

# 3. Set the remote VM URL
VM_URL="http://vm-host.example.com:8429"   # or https://...
sed -i "s|\${VM_REMOTE_URL}|$VM_URL|" config-datasource.yaml
# OR if using kustomize vars (see kustomization.yaml), set VM_REMOTE_URL there

# 4. Deploy
oc apply -k .

# 5. Wait for rollout
oc rollout status deploy/grafana -n rustfs-monitoring

# 6. Get the route URL
oc get route grafana -n rustfs-monitoring -o jsonpath='{.spec.host}{"\n"}'
# → grafana-rustfs-monitoring.apps.<cluster-domain>
```

## What gets deployed

| Resource | Purpose |
|---|---|
| `Namespace/rustfs-monitoring` | dedicated ns |
| `ServiceAccount/grafana` | pod identity (no special RBAC needed) |
| `Deployment/grafana` | Grafana 13.0.2, rootless, hardened |
| `Service/grafana` | ClusterIP :3000 |
| `Route/grafana` | edge TLS (cluster default cert) |
| `ConfigMap/grafana-datasources` | VictoriaMetrics datasource |
| `ConfigMap/grafana-dashboards-cfg` | dashboard provider config |
| `ConfigMap/grafana-dashboard-rustfs` | the single `RustFS` dashboard JSON |
| `ConfigMap/grafana-alerts` | 3 alert rules |
| `Secret/grafana-admin` | admin password |

## Hardening notes

The deployment is built to run under OpenShift's default `restricted-v2` SCC:

- `runAsNonRoot: true`, `runAsUser: 472` (Grafana's built-in UID)
- `allowPrivilegeEscalation: false`
- `readOnlyRootFilesystem: true` (Grafana's `/var/lib/grafana` is an `emptyDir`)
- `capabilities.drop: [ALL]`
- `seccompProfile.type: RuntimeDefault`

No privileged SCC is required.

## Optional: VM with HTTP basic auth

If you want VM to require credentials (e.g., when Grafana is in a different
trust boundary):

1. Edit `../systemd/etc/victoria-metrics.service` to add flags:
   ```
   -httpAuth.username=monitoring \
   -httpAuth.passwordFile=/etc/rustfs-mon/vm.password
   ```
2. In `config-datasource.yaml` add basic auth fields:
   ```yaml
   basicAuth: true
   basicAuthUser: monitoring
   ```
   with the password sourced from a Secret via env var on the pod.

## Verifying the deployment

```bash
# Health
oc rsh -n rustfs-monitoring deploy/grafana \
  curl -s http://localhost:3000/api/health
# → {"version":"13.0.2","database":"ok"}

# Datasource health (proves VM URL is reachable from the pod)
oc rsh -n rustfs-monitoring deploy/grafana \
  curl -s -u admin:$GRAFANA_PASS \
    http://localhost:3000/api/datasources/uid/PBFA97CFB590B2093/health
# → "Successfully queried the Prometheus API"

# External access
ROUTE=$(oc get route grafana -n rustfs-monitoring -o jsonpath='{.spec.host}')
echo "https://$ROUTE"
curl -k -s -u admin:$GRAFANA_PASS "https://$ROUTE/api/search?type=dash-db"
# → ["RustFS"]
```

## Dry-run (no cluster required)

If you have `oc` locally:

```bash
oc apply --dry-run=client -k .
# should list: namespace, serviceaccount, deployment, service, route, 4 configmaps, 1 secret
```

If you don't have `oc`, `kubectl` works against plain manifests:

```bash
kubectl apply --dry-run=client -f . --namespace=rustfs-monitoring
```

Note: the Route resource is OpenShift-specific. On plain Kubernetes it would
fail; convert to Ingress if needed.

## Troubleshooting

| Symptom | Fix |
|---|---|
| Datasource health says connection refused | OpenShift pod can't reach VM URL; check egress firewall |
| Dashboard not provisioned | Check `/var/lib/grafana/dashboards/rustfs.json` exists in pod (`oc rsh ... ls -la /var/lib/grafana/dashboards`) |
| Alert rules not loaded | Check Grafana UI → Alerting → Alert rules; if empty, `/etc/grafana/provisioning/alerting/rules.yaml` mount may be missing |
| Route returns 503 | Pod not ready yet; `oc get pods -n rustfs-monitoring` and check events |
| `image pull back-off` | Cluster can't reach Docker Hub; mirror `grafana/grafana:13.0.2` to internal registry and edit deployment |

## Uninstall

```bash
oc delete -k .
oc delete namespace rustfs-monitoring   # if no other resources inside
```