# RustFS 监控栈

单机部署，4 个容器：rustfs / exporter / vm / grafana。

## 启动

```bash
cp .env.example .env
# 编辑 .env 填入 RUSTFS_IMAGE / 凭证
./scripts/load-offline.sh   # 仅离线首次
docker compose up -d
```

## 端口

- 9000 — RustFS S3 + admin
- 9090 — rustfs-exporter /metrics
- 8429 — VictoriaMetrics PromQL
- 3000 — Grafana（默认 admin / admin）