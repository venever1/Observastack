# ARCHITECTURE — ObservaStack

## 1. High-Level Diagram (teks)
```
Client (React dashboard)
      |
      v
  Nginx/Traefik (reverse proxy, TLS)
      |
      v
  Backend API (Go/Node) ---> PostgreSQL (data, per-tenant via tenant_id)
      |         |
      |         +--> Redis (cache, rate limit, session)
      |
      +--/metrics--> Prometheus --> Grafana (dashboard)
      +--logs (JSON)--> Promtail --> Loki --> Grafana
      +--traces--> OpenTelemetry Collector --> Jaeger/Tempo
```

## 2. Komponen

| Komponen      | Peran                                              |
|---------------|-----------------------------------------------------|
| Backend API   | Business logic, auth, RBAC, expose /metrics         |
| PostgreSQL    | Data utama, tabel di-scope oleh tenant_id           |
| Redis         | Cache, session, rate limiter (token bucket)         |
| Prometheus    | Scrape metrics tiap 15s dari backend                |
| Grafana       | Visualisasi metrics + logs + traces                 |
| Loki+Promtail | Agregasi log terstruktur                            |
| Jaeger        | Distributed tracing per request                     |
| Nginx/Traefik | Reverse proxy, TLS termination, routing              |

## 3. Alur Request (contoh: create task)
1. Client kirim `POST /api/v1/tasks` dengan JWT di header.
2. Traefik terima, forward ke backend.
3. Middleware auth verifikasi JWT → extract `tenant_id`, `user_id`, `role`.
4. Middleware rate limiter cek Redis (key: `ratelimit:{tenant_id}`).
5. Middleware tracing mulai span baru, propagate trace context.
6. Handler cek RBAC (role cukup?) → simpan ke Postgres (dengan `tenant_id`).
7. Middleware metrics catat durasi + status code ke Prometheus.
8. Logger tulis structured log (JSON) dengan `trace_id`, `tenant_id`, `latency`.
9. Response dikirim balik ke client.

## 4. Prinsip Desain
- **Tenant isolation by default**: setiap query WAJIB filter `tenant_id`. Tidak ada endpoint yang skip ini.
- **Observability bukan tempelan**: metrics/log/trace dipasang di level middleware, bukan manual per handler.
- **Trace berlapis untuk alur kritis**: selain span request dari middleware, alur auth menambah child span —
  query PostgreSQL/Redis otomatis via instrumentasi driver, langkah non-DB (verifikasi password, pembuatan
  token) via span manual `observastack/auth`. Tanpa data sensitif di attribute span (lihat ADR-003).
- **Stateless backend**: semua state di Postgres/Redis, backend bisa di-scale horizontal kapan saja.
- **Fail loud, log detail**: error harus punya `trace_id` yang bisa dilacak balik ke log & trace.

## 5. Struktur Folder Backend (usulan)
```
/cmd/api            → entrypoint
/internal/auth       → JWT, RBAC middleware
/internal/tenant     → tenant middleware & resolver
/internal/task       → domain: handler, service, repository
/internal/observability → setup prometheus, otel, logger
/internal/config     → load env
/migrations          → SQL migration files
/deploy
  /docker-compose.yml
  /prometheus.yml
  /grafana/dashboards/
  /loki-config.yml
  /otel-collector-config.yml
```

## 6. Keputusan Arsitektur Kunci
Lihat `docs/DECISIONS.md` untuk alasan di balik setiap pilihan besar (mis. kenapa shared-table bukan schema-per-tenant).
