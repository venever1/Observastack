# PRD — ObservaStack

## 1. Latar Belakang & Tujuan
ObservaStack adalah backend SaaS multi-tenant yang dilengkapi observability stack production-grade
(metrics, logging, tracing). Tujuan project: portfolio yang membuktikan kemampuan backend + DevOps,
bukan sekadar CRUD API.

## 2. Target Pengguna
- Tenant (perusahaan/tim) yang mendaftar dan mengelola task/inventory dalam workspace terisolasi.
- Admin platform yang memantau kesehatan sistem lewat Grafana.

## 3. Problem Statement
Kebanyakan project portfolio backend berhenti di "CRUD + auth". Tidak ada bukti kemampuan
mengoperasikan sistem di production: memantau error, melacak request lambat, dan multi-tenancy yang aman.

## 4. Scope (MVP)
### In Scope
- Auth: register/login, JWT + refresh token
- Multi-tenancy: 1 user bisa punya 1+ tenant, data terisolasi per tenant_id
- RBAC: role admin / member per tenant
- CRUD resource utama (contoh: Task) dengan rate limiting per tenant
- Observability: metrics (Prometheus), logs (Loki), tracing (Jaeger) aktif di semua endpoint
- Dashboard Grafana: request rate, error rate, latency p50/p95/p99, usage per tenant
- Deploy via Docker Compose, CI/CD dasar via GitHub Actions

### Out of Scope (v1)
- Billing/payment
- Real-time collaboration (websocket)
- Mobile app
- Kubernetes (jadi stretch goal, bukan wajib MVP)

## 5. User Stories
- Sebagai tenant owner, saya bisa daftar dan otomatis punya workspace sendiri.
- Sebagai member, saya bisa CRUD task hanya di dalam tenant saya.
- Sebagai admin platform, saya bisa lihat dashboard Grafana untuk memantau seluruh tenant.
- Sebagai developer, saya bisa trace 1 request dari masuk sampai selesai lewat Jaeger.
- Sebagai developer, saya bisa cari error tertentu lewat Loki tanpa SSH ke server.

## 6. Success Metrics
- Semua endpoint punya coverage metrics + log + trace (100%)
- p99 latency endpoint utama < 300ms saat load test 100 concurrent users
- Dashboard Grafana bisa menjawab: "tenant mana paling banyak request 1 jam terakhir?"
- CI/CD berhasil build-test-deploy otomatis tanpa langkah manual

## 7. Konstrain
- Budget: development harus $0 (local Docker). Demo publik pakai Oracle Cloud Free Tier.
- Waktu: 6–8 minggu part-time.
- Tim: solo developer (dibantu AI coding agent).

## 8. Open Questions
- Domain resource utama: Task management atau Inventory? → default: Task management.
- Schema-per-tenant atau shared table + tenant_id? → default: shared table (lebih sederhana untuk MVP).
