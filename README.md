# ObservaStack

SaaS backend multi-tenant dengan full observability stack (metrics, logs, tracing) — dibangun untuk
menunjukkan kemampuan backend engineering + DevOps production-grade, bukan sekadar CRUD API.

## Dokumentasi
Semua dokumen kebutuhan project ada di folder `docs/`:

| File | Isi |
|------|-----|
| `docs/PRD.md` | Requirement produk: tujuan, scope, user stories |
| `docs/ARCHITECTURE.md` | Desain sistem, alur request, prinsip desain |
| `docs/TECH_STACK.md` | Daftar teknologi & versi yang dipakai |
| `docs/API_SPEC.md` | Kontrak API (endpoint, request/response, error code) |
| `docs/DATABASE_SCHEMA.md` | Struktur tabel database |
| `docs/CODING_STANDARDS.md` | Konvensi kode & checklist review |
| `docs/ROADMAP.md` | Rencana kerja mingguan & status task |
| `docs/DECISIONS.md` | Histori keputusan teknis (ADR) |

Kalau kamu pakai AI coding agent (Claude Code, Cursor, dll), agent akan otomatis baca `AGENTS.md`
di root repo ini sebagai entry point konteks.

## Quickstart
```bash
cp .env.example .env
docker compose up -d
docker compose logs -f app
```

Setelah semua service jalan:
- API: http://localhost:8080
- Grafana: http://localhost:3000 (default admin/admin)
- Prometheus: http://localhost:9090
- Jaeger UI: http://localhost:16686

## Struktur Repo
```
.
├── AGENTS.md              # entry point konteks untuk AI agent
├── README.md
├── .env.example
├── docs/                  # semua dokumen requirement & arsitektur
├── deploy/                # docker-compose, config prometheus/loki/grafana
├── cmd/                   # entrypoint aplikasi
├── internal/              # source code utama
└── migrations/            # SQL migration
```

## Status
Lihat `docs/ROADMAP.md` untuk progres terkini.
