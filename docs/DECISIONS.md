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
