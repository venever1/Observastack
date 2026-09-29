# DECISIONS (ADR Log) — ObservaStack

> Catat keputusan teknis penting di sini. Format singkat, agent bisa baca ini untuk paham "kenapa" sebelum mengubah sesuatu yang terlihat aneh.

## Template
```
## ADR-XXX: <judul keputusan>
Tanggal: YYYY-MM-DD
Status: Diterima / Ditolak / Digantikan oleh ADR-YYY

### Konteks
Apa masalah yang perlu diputuskan?

### Keputusan
Apa yang diputuskan?

### Alasan
Kenapa opsi ini dipilih dibanding yang lain?

### Konsekuensi
Apa trade-off yang diterima?
```

---

## ADR-001: Shared table + tenant_id, bukan schema-per-tenant
Tanggal: 2026-09-19
Status: Diterima

### Konteks
Perlu memilih strategi multi-tenancy: shared table dengan kolom tenant_id, atau schema terpisah per tenant.

### Keputusan
Pakai shared table dengan kolom `tenant_id` di setiap tabel domain.

### Alasan
Lebih sederhana untuk MVP, migration lebih mudah dikelola (1 skema untuk semua tenant),
cukup untuk skala portfolio project. Schema-per-tenant menambah kompleksitas operasional
yang tidak sebanding dengan manfaatnya di tahap ini.

### Konsekuensi
Perlu disiplin ketat: SETIAP query harus filter tenant_id, atau data bisa bocor antar tenant.
Kalau di masa depan butuh isolasi lebih ketat (compliance, dsb), migrasi ke schema-per-tenant
akan butuh effort besar.

---

## ADR-002: Oracle Cloud Free Tier untuk demo publik
Tanggal: 2026-09-19
Status: Diterima

### Konteks
Butuh tempat deploy demo publik untuk full observability stack tanpa biaya.

### Keputusan
Pakai Oracle Cloud Free Tier (4 ARM vCPU, 24GB RAM, gratis permanen).

### Alasan
Free tier lain (AWS/GCP) hanya trial dengan kredit terbatas dan instance kecil, tidak cukup
untuk menjalankan Postgres + Redis + Prometheus + Grafana + Loki + Jaeger sekaligus.

### Konsekuensi
Setup awal Oracle Cloud sedikit lebih ribet (approval akun kadang butuh waktu),
dan arsitektur ARM perlu dipastikan semua image Docker punya build ARM64.

---

## ADR-003: Child span untuk alur auth (driver-level + manual)
Tanggal: 2026-09-29
Status: Diterima

### Konteks
Trace `POST /auth/login` hanya 1 span (~300ms) dari TracerMiddleware. Tidak
kelihatan waktu habis di mana: query PostgreSQL, call Redis (rate limiter),
verifikasi bcrypt, atau pembuatan token.

### Keputusan
- Query PostgreSQL otomatis jadi span via `otelpgx.Tracer` di
  `config.OpenPostgres` (`github.com/exaring/otelpgx`). Parameter query tidak
  pernah dicatat (opsi `WithIncludeQueryParameters` sengaja tidak dipakai);
  teks statement yang tercatat hanya berisi placeholder `$1` karena semua query
  auth terparameterisasi.
- Call Redis otomatis jadi span via `redisotel.InstrumentTracing` di
  `config.OpenRedis`, dengan `WithDBStatement(false)` karena key rate-limit
  mengandung subjek lookup (email/IP) yang tidak boleh masuk attribute span.
- Langkah non-DB jadi span manual dengan tracer `observastack/auth` di
  `internal/auth`: `auth.hash_password`, `auth.verify_password`,
  `auth.generate_access_token`, `auth.issue_refresh_token`, plus
  `auth.create_account` yang membungkus transaksi registrasi. Setiap error di
  span dicatat via `span.RecordError` + `span.SetStatus(codes.Error, ...)`.
- Tidak ada attribute/event span yang membawa data sensitif: password, hash,
  access/refresh token, email.

### Alasan
`opentelemetry-go-contrib` tidak menyediakan instrumentasi pgx/v5, jadi dipakai
`github.com/exaring/otelpgx` (standar komunitas, 110+ importer). Untuk go-redis,
instrumentasi resmi (`extra/redisotel/v9`) sudah satu rilis dengan client yang
dipakai (v9.22.0). Span manual hanya untuk yang tidak ter-cover driver
(bcrypt/JWT), bukan duplikasi query DB.

### Konsekuensi
Satu dependency baru (`otelpgx`); `redisotel`/`rediscmd` ikut modul go-redis
yang sudah ada. Konteks `r.Context()` sudah mengalir handler -> service ->
repository sejak awal (termasuk ke closure `InTx`), jadi tidak ada perubahan
perilaku/API — hanya span tambahan di Jaeger.
