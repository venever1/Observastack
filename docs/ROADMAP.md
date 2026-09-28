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

## Minggu 5: Rate Limiting
- [x] Redis client di `main.go` (fail-fast saat startup, close saat graceful shutdown)
- [x] Rate limiter atomic via Lua script (ganti `INCR` + `EXPIRE` non-atomik)
- [x] Limit per-IP **dan** per-email untuk `/auth/login` (AND, bukan OR)
- [x] Limit per-IP untuk `/auth/register`
- [x] HTTP 429 + header `Retry-After` dari sisa TTL yang sebenarnya
- [x] `X-Forwarded-For` hanya dipercaya bila `TRUSTED_PROXY=true`
- [ ] Limit per-tenant untuk route yang sudah terautentikasi (butuh `tenant.Resolver` aktif di route)

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
- ~~Rate limiting belum terpasang.~~ **Sudah selesai** — lihat bagian "Minggu 5: Rate Limiting".
  Catatan operasional: `TRUSTED_PROXY` **wajib** `true` di produksi karena Traefik
  berada di depan app (`docs/ARCHITECTURE.md`). Kalau dibiarkan `false` di produksi,
  semua request akan terlihat berasal dari satu IP proxy sehingga limit per-IP
  berlaku global untuk semua user.
- **Deploy demo publik: `TRUSTED_PROXY` WAJIB diubah.** Kedua compose file di repo
  ini memakai `TRUSTED_PROXY=false` dan **tidak** mendefinisikan Traefik/nginx —
  app dipublish langsung ke host. `false` benar untuk stack tersebut.
  Saat deploy ke VPS di belakang Traefik (`docs/ARCHITECTURE.md:31`), set
  `TRUSTED_PROXY=true`, karena jika tidak semua request terlihat berasal dari satu
  IP proxy dan limit per-IP jadi global untuk semua user.
  JANGAN pernah `true` bila app bisa dijangkau langsung dari internet: header
  `X-Forwarded-For` dikontrol klien, jadi limit per-IP bisa di-bypass sepenuhnya.
- ~~**SA1029 (`staticcheck`) masih ditunda.**~~ **Sudah selesai** — context key
  tenant sekarang hidup di `internal/requestctx`, satu package leaf yang hanya
  mengekspor `WithTenantID` dan `TenantIDFrom`; tipe key-nya unexported, jadi
  tidak ada package lain yang bisa membuat atau memalsukan key tersebut.
  Exclusion SA1029 di `.golangci.yml` sudah **dihapus**; `golangci-lint` bersih
  tanpa exclusion apa pun.
  - **Ini memperbaiki bug, bukan sekadar rename.** Sebelumnya logger membaca key
    `string` polos `"tenantID"` sementara `tenant.Resolver` menulis key bertipe
    miliknya sendiri. Keduanya key yang berbeda, jadi field `tenant_id` di log
    **selalu kosong**. Dibuktikan dengan test end-to-end di
    `internal/tenant/tenantid_logging_test.go` yang memakai `tenant.Resolver`
    asli sebagai producer dan logger asli sebagai consumer.
  - **Belum terverifikasi di runtime.** `tenant.Resolver` belum dipasang ke route
    mana pun di `cmd/api/main.go`, jadi belum ada request produksi yang melewati
    chain `Auth -> Resolver -> handler`. Test membuktikan alurnya benar di level
    middleware, tetapi `tenant_id` baru bisa terlihat benar-benar terisi di log
    Loki setelah Resolver dipasang ke route tenant-scoped. Lihat bagian
    "Minggu 5: Rate Limiting" untuk item limit per-tenant yang bergantung pada ini.
  - `userIDKey` di `internal/auth` masih memakai `type contextKey string` lokal
    dan **belum** dipindah ke `internal/requestctx`. Itu di luar scope perubahan
    ini. Tetap jadi risiko kalau nanti ditambah context key baru di package itu;
    ada baiknya dipindah sekaligus saat route tenant-scoped mulai dipasang.
- `make` tidak tersedia di environment Windows proyek ini; jalankan `./cmd/migrate`
  langsung via `go run ./cmd/migrate up|down`.
- Test integrasi butuh dependency eksternal dan di-skip secara default. Jalankan:
  ```
  INTEGRATION_DATABASE_URL='postgres://observastack:observastack@localhost:5432/observastack?sslmode=disable' go test ./internal/auth/ -run Integration
  INTEGRATION_REDIS_URL='redis://localhost:6379' go test ./internal/middleware/ -run IntegrationRateLimit
  ```
