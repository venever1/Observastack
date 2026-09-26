# API SPEC — ObservaStack

> Kontrak ini WAJIB disinkronkan setiap ada perubahan endpoint. Agent harus update file ini di PR yang sama dengan perubahan kode.

## Konvensi Umum
- Base path: `/api/v1`
- Auth: `Authorization: Bearer <jwt>`
- Semua response sukses: `{ "data": ..., "meta": {...} }`
- Semua response error: `{ "error": { "code": "STRING_CODE", "message": "..." } }`
- Semua list endpoint support pagination: `?page=1&limit=20`

## Auth
| Method | Path                  | Deskripsi                       | Auth |
|--------|-----------------------|----------------------------------|------|
| POST   | /auth/register        | Daftar user baru + buat tenant   | No   |
| POST   | /auth/login            | Login, dapat access+refresh token| No   |
| POST   | /auth/refresh          | Refresh access token             | No   |
| POST   | /auth/logout           | Invalidate refresh token         | Yes  |

## Tenant
| Method | Path                  | Deskripsi                       | Auth       |
|--------|-----------------------|----------------------------------|------------|
| GET    | /tenants/me            | Info tenant aktif                | Yes        |
| POST   | /tenants/invite        | Invite member baru               | Yes (admin)|

## Task (resource utama contoh)
| Method | Path              | Deskripsi              | Auth | Role   |
|--------|-------------------|-------------------------|------|--------|
| GET    | /tasks            | List task tenant aktif  | Yes  | any    |
| POST   | /tasks            | Buat task baru           | Yes  | any    |
| GET    | /tasks/:id        | Detail task              | Yes  | any    |
| PATCH  | /tasks/:id        | Update task              | Yes  | any    |
| DELETE | /tasks/:id        | Hapus task               | Yes  | admin  |

## Observability (internal, tidak untuk client)
| Method | Path      | Deskripsi                          |
|--------|-----------|--------------------------------------|
| GET    | /metrics  | Expose metrics format Prometheus     |
| GET    | /healthz  | Health check (liveness)              |
| GET    | /readyz   | Readiness check (DB/Redis connected) |

## Error Codes Standar
| Code                | HTTP Status | Arti                          |
|---------------------|-------------|--------------------------------|
| UNAUTHORIZED         | 401         | Token invalid/expired          |
| FORBIDDEN            | 403         | Role tidak cukup               |
| TENANT_MISMATCH      | 403         | Resource bukan milik tenant ini|
| RATE_LIMITED         | 429         | Melebihi limit request         |
| VALIDATION_ERROR     | 422         | Input tidak valid              |
| NOT_FOUND            | 404         | Resource tidak ditemukan       |
| INTERNAL_ERROR       | 500         | Error tak terduga              |
| PAYLOAD_TOO_LARGE    | 413         | Request body melebihi batas ukuran (`MAX_REQUEST_BODY_SIZE`) |

## Catatan untuk Agent
- Setiap endpoint baru wajib ditambahkan ke tabel di atas SEBELUM merge.
- Jangan buat error code baru tanpa menambahkannya ke tabel Error Codes.
