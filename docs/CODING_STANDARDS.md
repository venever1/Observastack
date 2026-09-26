# CODING STANDARDS — ObservaStack

## Prinsip Umum
- Konsistensi > preferensi pribadi. Ikuti pola yang sudah ada di repo, jangan bikin pola baru tanpa alasan kuat.
- Setiap fungsi publik butuh comment singkat kalau logikanya tidak trivial.
- Tidak ada magic number/string — pakai constant bernama.

## Struktur Kode (Go, sesuaikan kalau pakai stack lain)
- 1 domain = 1 folder di `/internal` (handler, service, repository terpisah)
- Handler: hanya urus HTTP (parse request, panggil service, format response)
- Service: business logic murni, tidak tahu soal HTTP
- Repository: hanya query DB, tidak ada business logic

## Error Handling
- Semua error dari layer bawah di-wrap dengan context (`fmt.Errorf("create task: %w", err)`)
- Error yang dikirim ke client HARUS memakai Error Codes dari `docs/API_SPEC.md`
- Jangan pernah expose stack trace/internal error message mentah ke client

## Naming
- Tabel DB: snake_case, jamak (`tasks`, `tenant_members`)
- JSON field di response: camelCase
- Env var: UPPER_SNAKE_CASE
- Branch git: `feature/nama-fitur`, `fix/nama-bug`

## Observability Wajib per Endpoint Baru
Setiap handler baru WAJIB:
1. Tercatat di Prometheus (via middleware, otomatis — jangan skip middleware)
2. Log terstruktur JSON minimal berisi: `trace_id`, `tenant_id`, `method`, `path`, `status`, `latency_ms`
3. Trace span aktif (otomatis via middleware OpenTelemetry)

## Testing
- Unit test wajib untuk service layer (business logic)
- Integration test untuk repository (pakai test container Postgres)
- Minimal 1 test happy-path + 1 test error-path per endpoint baru

## Commit Message
Format: `type(scope): deskripsi singkat`
Contoh: `feat(task): add pagination to list endpoint`, `fix(auth): handle expired refresh token`
Type valid: feat, fix, refactor, docs, test, chore

## Review Checklist Sebelum PR
- [ ] Tenant isolation sudah benar (query filter tenant_id)?
- [ ] Metrics/log/trace sudah aktif?
- [ ] API_SPEC.md sudah diupdate kalau ada endpoint baru?
- [ ] Migration sudah ada kalau ada perubahan skema?
- [ ] Test sudah ditambahkan?
