# ROADMAP — ObservaStack

> Update file ini setiap task selesai/dimulai. Agent WAJIB cek bagian "Sedang Dikerjakan" sebelum mulai kerja.

## Sedang Dikerjakan
- [ ] Load testing dengan k6, generate data realistis di dashboard

## Minggu 1–2: Core Backend
- [x] Setup project skeleton + struktur folder
- [x] Setup Postgres connection + migration tool
- [x] Model & migration: tenants, users, tenant_members
- [x] Auth service: register, login, refresh, logout, JWT + refresh token
- [x] Auth HTTP wiring: `auth.Handler` terhubung ke mux (`/auth/*`), plus tenant + membership dibuat saat register
- [x] Middleware: auth verify, tenant resolver, RBAC

## Minggu 3: Observability Dasar
- [x] Integrasi Prometheus client, expose `/metrics`
- [x] Middleware metrics (request count, duration, status)
- [x] Setup Grafana + datasource Prometheus
- [x] Dashboard pertama: request rate & error rate

## Minggu 4: Logging & Tracing
- [x] Structured logger (JSON) dengan trace_id
- [x] Setup Loki + Promtail, kirim log dari container
- [x] Integrasi OpenTelemetry SDK
- [x] Setup Jaeger, trace minimal 2-3 endpoint kritis

## Minggu 5: Containerization
- [x] Dockerfile backend (multi-stage build)
- [x] docker-compose.yml full stack (app, db, redis, prometheus, grafana, loki, jaeger)
- [x] Health check & readiness probe

## Minggu 6: CI/CD
- [x] GitHub Actions: lint + test on PR
- [x] GitHub Actions: build & push image on merge ke main

## Minggu 7–8: Polish & Deploy
- [ ] Load testing dengan k6, generate data realistis di dashboard
- [ ] Setup Oracle Cloud Free Tier
- [ ] Deploy demo publik + domain + TLS
- [ ] Tulis README lengkap + architecture diagram visual

## Backlog / Stretch Goals
- [ ] Migrasi ke k3s (nunjukin skill K8s)
- [ ] ArgoCD untuk GitOps deployment
- [ ] Alerting via Alertmanager → Slack/Discord
- [ ] Multi-region read replica Postgres

## Known Issues
- **Rate limiting belum terpasang.** `internal/auth/ratelimit.go` sudah ada
  (Redis fixed-window, key `ratelimit:login:<email>`) tapi belum di-wire, belum ada
  client Redis di `main.go`, dan belum ada HTTP 429 + `Retry-After`. Catatan: key-nya
  hanya per-email (bukan per-IP) dan `Incr`+`Expire` tidak atomik.
- `TestMiddleware_CountsRequests` (`internal/observability/metrics_test.go`) tidak
  idempoten: gagal bila dijalankan dengan `go test -count=2` karena membandingkan
  counter Prometheus global dengan literal `1`.
- `make` tidak tersedia di environment Windows proyek ini; jalankan `./cmd/migrate`
  langsung via `go run ./cmd/migrate up|down`.
