# TECH STACK — ObservaStack

## Backend
- Bahasa: Go 1.22+ (alternatif: Node.js 20 + NestJS)
- Framework: chi/gin (Go) atau NestJS (Node)
- ORM/Query: sqlc atau GORM (Go) / Prisma (Node)
- Validasi: go-playground/validator atau class-validator

## Database & Cache
- PostgreSQL 16
- Redis 7

## Observability
- Prometheus (metrics scraping)
- Grafana (dashboard, versi terbaru)
- Loki + Promtail (log aggregation)
- Jaeger atau Grafana Tempo (tracing)
- OpenTelemetry SDK (instrumentasi)

## Infra & DevOps
- Docker + Docker Compose (local & demo)
- Traefik (reverse proxy + auto TLS via Let's Encrypt)
- GitHub Actions (CI/CD)
- k3s (opsional, stretch goal untuk demo K8s)
- Load testing: k6

## Frontend (minim, sekadar penunjang)
- React + Vite
- TailwindCSS
- TanStack Query buat data fetching

## Deployment Target
- Local: Docker Compose
- Demo publik: Oracle Cloud Free Tier (4 vCPU ARM / 24GB RAM, gratis permanen)

## Versi Wajib Dicatat
Setiap kali upgrade versi major dependency, catat di `docs/DECISIONS.md` alasan upgrade-nya.
