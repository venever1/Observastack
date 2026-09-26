# AGENTS.md — Panduan untuk AI Coding Agent

> File ini adalah entry point. Baca file ini duluan sebelum ngerjain task apapun di repo ini.

## Tentang Project
ObservaStack — SaaS backend multi-tenant dengan full observability stack (metrics, logs, tracing).
Baca `docs/PRD.md` untuk detail requirement dan `docs/ARCHITECTURE.md` untuk detail teknis sistem.

## Urutan Baca Dokumen (WAJIB sebelum coding)
1. `docs/PRD.md` — apa yang dibangun dan kenapa
2. `docs/ARCHITECTURE.md` — bagaimana sistem disusun
3. `docs/TECH_STACK.md` — teknologi & versi yang dipakai
4. `docs/API_SPEC.md` — kontrak API yang harus dipatuhi
5. `docs/DATABASE_SCHEMA.md` — struktur data
6. `docs/CODING_STANDARDS.md` — konvensi kode di repo ini
7. `docs/ROADMAP.md` — task mana yang lagi dikerjain sekarang

## Aturan Kerja Agent
- Jangan ubah `docs/ARCHITECTURE.md` atau `docs/DATABASE_SCHEMA.md` tanpa persetujuan eksplisit — itu adalah source of truth.
- Setiap fitur baru harus multi-tenant aware (selalu filter by `tenant_id`).
- Semua endpoint baru WAJIB expose metrics ke Prometheus (`/metrics`) dan structured log (JSON) ke Loki.
- Jangan hardcode secret. Selalu pakai `.env` (lihat `.env.example`).
- Setelah menambah/mengubah endpoint, update `docs/API_SPEC.md` di PR yang sama.
- Setelah mengubah skema DB, tulis migration file — jangan edit tabel langsung.
- Ikuti `docs/CODING_STANDARDS.md` untuk struktur folder, naming, dan pattern error handling.

## Perintah Penting
```bash
docker compose up -d          # jalankan seluruh stack (app, db, redis, prometheus, grafana, loki, jaeger)
docker compose logs -f app    # lihat log service utama
make test                     # jalankan test suite
make migrate                  # jalankan migration DB
make lint                     # cek code style
```

## Status Project Saat Ini
Lihat `docs/ROADMAP.md` bagian "Sedang Dikerjakan" untuk tahu task aktif.
Lihat `docs/DECISIONS.md` untuk histori keputusan teknis penting (ADR).
