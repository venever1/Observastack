# DATABASE SCHEMA — ObservaStack

> Source of truth untuk struktur data. Perubahan skema HARUS lewat migration file di `/migrations`, tidak boleh edit manual di DB.

## Prinsip
- Semua tabel domain (bukan tabel referensi global) WAJIB punya kolom `tenant_id`.
- Semua tabel punya `id UUID`, `created_at`, `updated_at`.
- Soft delete pakai `deleted_at` (nullable), bukan hard delete, kecuali dinyatakan lain.

## Tabel: tenants
| Kolom       | Tipe      | Keterangan            |
|-------------|-----------|------------------------|
| id          | UUID PK   |                        |
| name        | TEXT      |                        |
| plan        | TEXT      | free/pro (untuk future)|
| created_at  | TIMESTAMPTZ |                      |

## Tabel: users
| Kolom          | Tipe      | Keterangan                  |
|----------------|-----------|------------------------------|
| id             | UUID PK   |                              |
| email          | TEXT UNIQUE |                            |
| password_hash  | TEXT      | bcrypt/argon2                |
| created_at     | TIMESTAMPTZ |                            |

## Tabel: tenant_members
| Kolom       | Tipe      | Keterangan                     |
|-------------|-----------|----------------------------------|
| id          | UUID PK   |                                  |
| tenant_id   | UUID FK → tenants.id             |
| user_id     | UUID FK → users.id               |
| role        | TEXT      | admin / member                   |
| created_at  | TIMESTAMPTZ |                                |

> Index unik: (tenant_id, user_id)

## Tabel: tasks
| Kolom       | Tipe      | Keterangan                  |
|-------------|-----------|------------------------------|
| id          | UUID PK   |                              |
| tenant_id   | UUID FK → tenants.id  | WAJIB, index               |
| title       | TEXT      |                              |
| status      | TEXT      | todo/in_progress/done       |
| created_by  | UUID FK → users.id    |                             |
| created_at  | TIMESTAMPTZ |                            |
| updated_at  | TIMESTAMPTZ |                            |
| deleted_at  | TIMESTAMPTZ NULLABLE |                    |

> Index: (tenant_id, status), (tenant_id, deleted_at)

## Tabel: refresh_tokens
| Kolom       | Tipe      | Keterangan            |
|-------------|-----------|------------------------|
| id          | UUID PK   |                        |
| user_id     | UUID FK → users.id     |
| token_hash  | TEXT      |                        |
| expires_at  | TIMESTAMPTZ |                      |
| revoked     | BOOLEAN DEFAULT false |                       |

## Aturan Migration untuk Agent
- Nama file: `YYYYMMDDHHMMSS_deskripsi_singkat.sql`
- Setiap migration harus punya `up` dan `down`.
- Jangan pernah `DROP COLUMN` tanpa backup plan — tambah migration terpisah untuk deprecate dulu.
