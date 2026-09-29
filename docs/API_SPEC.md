# API SPEC - ObservaStack

> Kontrak ini WAJIB disinkronkan setiap ada perubahan endpoint. Agent harus update file ini di PR yang sama dengan perubahan kode.
> Status: bagian **Auth** dan **Observability** sudah diimplementasikan. Bagian **Tenant** dan **Task** masih *planned*.

## Konvensi Umum
- Base path: tidak ada prefix (contoh: `/auth/login`). Rencana: pindah ke `/api/v1` saat endpoint resource ditambahkan.
- Auth (untuk endpoint yang butuh login): `Authorization: Bearer <accessToken>`
- Response sukses: `{ "data": ... }`
- Response error: `{ "error": { "code": "STRING_CODE", "message": "..." } }`
- Nama field JSON memakai camelCase.
- Pagination (`?page=1&limit=20`): *planned*, berlaku untuk endpoint list.

## Auth (implemented)
| Method | Path           | Body                           | Response `data`                       | Auth |
|--------|----------------|--------------------------------|----------------------------------------|------|
| POST   | /auth/register | `{email, password}`            | `{id, email, createdAt, tenantId}`     | No   |
| POST   | /auth/login    | `{email, password}`            | `{accessToken, refreshToken}`          | No   |
| POST   | /auth/refresh  | `{refreshToken}`               | token baru                             | No   |
| POST   | /auth/logout   | `{refreshToken}`               | -                                      | Yes  |

Catatan:
- Register membuat user baru beserta tenant-nya.
- `/auth/register` dan `/auth/login` dibatasi rate limiter (per IP; login juga per email). Melebihi batas mengembalikan `429 RATE_LIMITED`.

## Tenant (planned)
| Method | Path            | Deskripsi            | Auth        |
|--------|-----------------|----------------------|-------------|
| GET    | /tenants/me     | Info tenant aktif    | Yes         |
| POST   | /tenants/invite | Invite member baru   | Yes (admin) |

## Task (planned)
| Method | Path       | Deskripsi              | Auth | Role  |
|--------|------------|------------------------|------|-------|
| GET    | /tasks     | List task tenant aktif | Yes  | any   |
| POST   | /tasks     | Buat task baru         | Yes  | any   |
| GET    | /tasks/:id | Detail task            | Yes  | any   |
| PATCH  | /tasks/:id | Update task            | Yes  | any   |
| DELETE | /tasks/:id | Hapus task             | Yes  | admin |

## Observability (implemented, internal, tidak untuk client)
| Method | Path     | Deskripsi                           | Tracing |
|--------|----------|-------------------------------------|---------|
| GET    | /metrics | Expose metrics format Prometheus    | Ya      |
| GET    | /healthz | Health check (liveness)             | Tidak   |
| GET    | /readyz  | Readiness check (DB/Redis)          | Tidak   |

## Error Codes Standar
| Code              | HTTP Status | Arti                                                          |
|-------------------|-------------|---------------------------------------------------------------|
| UNAUTHORIZED      | 401         | Token invalid/expired                                         |
| FORBIDDEN         | 403         | Role tidak cukup                                              |
| TENANT_MISMATCH   | 403         | Resource bukan milik tenant ini                               |
| RATE_LIMITED      | 429         | Melebihi limit request                                        |
| VALIDATION_ERROR  | 422         | Input tidak valid                                             |
| NOT_FOUND         | 404         | Resource tidak ditemukan                                      |
| INTERNAL_ERROR    | 500         | Error tak terduga                                             |
| PAYLOAD_TOO_LARGE | 413         | Request body melebihi batas ukuran (`MAX_REQUEST_BODY_SIZE`)  |

## Catatan untuk Agent
- Setiap endpoint baru wajib ditambahkan ke tabel di atas SEBELUM merge, dengan status implemented/planned.
- Jangan buat error code baru tanpa menambahkannya ke tabel Error Codes.
