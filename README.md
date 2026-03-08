# GovTech Christmas Redemption System

A Go-based API for managing Christmas gift redemptions across multiple teams. Uses PostgreSQL as the source of truth and Redis as a fast cache layer with atomic SETNX-based concurrency control for multi-desk environments.

## Assessment Disclaimer

> **This project is built for a technical assessment and is not fully production-ready.**
>
> Key limitation: `init.sql` contains sample data with `INSERT INTO redemptions ... ('Team Alpha', TRUE), ('Team Beta', TRUE)`. Every time the Docker PostgreSQL container is created from scratch (e.g. after `docker-compose down -v` or deleting the volume), the database is re-initialised with these rows — meaning those teams start as "already redeemed". The Redis cache is then pre-warmed from this state.
>
> In a production system:
> - `init.sql` would only create the schema and indexes, not insert sample data. The database would also probably exist beforehand and persist across instances of this service. The only programs constantly being reset would be the golang services and the redis cache.
> - Database migrations would be managed by a tool like `golang-migrate` or `goose`
> - Redis would use a password and TLS
> - Secrets would not be hardcoded in `docker-compose.yml` or `.env`
> - The health check endpoint would not expose internal service names
>
> These trade-offs were made intentionally to keep the assessment submission self-contained and easy to spin up with a single `docker-compose up`.

## Architecture

```
govtech-christmas/
├── main.go                    # HTTP API server, DB/Redis init, CSV loading, cache prewarm
├── main_test.go               # 8 unit tests (CSV parsing, env helpers)
├── integration_test.go        # 34 integration tests (real PostgreSQL + Redis via Docker)
├── api/
│   ├── types.go               # App struct, data models (StaffMapping, Redemption)
│   ├── handlers.go            # HTTP endpoint handlers, health check
│   ├── service.go             # Business logic (RedeemPresent, CheckEligibility)
│   └── cache/
│       ├── store.go           # CacheStore interface
│       └── redis.go           # Redis implementation (SETNX, TTL)
├── data/
│   └── staff_mappings.csv     # Staff-to-team mappings (loaded on startup)
├── docker-compose.yml         # PostgreSQL + Redis + App services
├── Dockerfile                 # Multi-stage Go build
├── init.sql                   # DB schema, indexes, sample data
├── go.mod / go.sum            # Go module dependencies
└── .env                       # Environment variables
```

## How It Works

### Data Flow

1. **Startup**: PostgreSQL tables created via `init.sql` → CSV staff mappings loaded into DB → Redis cache pre-warmed with all staff-to-team mappings
2. **Eligibility check**: Cache-aside lookup (Redis hit → return, miss → DB query → populate cache)
3. **Redemption**: Staff lookup (cache-aside) → `SETNX` atomic gate in Redis (only first desk wins) → write to PostgreSQL → on DB failure, rollback cache via `InvalidateRedemption`
4. **Reversal**: `DELETE /api/v1/redemptions/:team_name` removes from DB then invalidates Redis cache

### Redis Cache Strategy

| Concern | Approach |
|---------|----------|
| Staff lookups | Cache-aside with 1h TTL (static data, pre-warmed on startup) |
| Redemption status | SETNX atomic gate with 24h TTL |
| Concurrent desks | Redis `SETNX` — single-threaded, exactly one desk wins |
| Write order | PostgreSQL first (durability), then Redis (performance) |
| DB write failure | Redis key rolled back via `InvalidateRedemption` |
| Ops reversal | DELETE endpoint invalidates cache immediately |
| Redis down | System degrades gracefully to DB-only reads; `/health` reports `"degraded"` |

## Quick Start

### Prerequisites

- Docker & Docker Compose
- Go 1.25+ (for local development / running tests)

### Run with Docker Compose

```bash
docker-compose up -d
```

This starts PostgreSQL, Redis, and the API server. The API is available at `http://localhost:8080`.

### Run Locally (development)

```bash
# Start dependencies
docker-compose up postgres redis -d

# Run the server
go run main.go

# Run tests
go test -v ./...
```

## API Endpoints

### Health Check

```
GET /health
```

Returns DB and cache health:

```json
{
  "status": "healthy",
  "service": "govtech-christmas-redemption",
  "database": true,
  "cache": true
}
```

Status values: `"healthy"` (all systems up), `"degraded"` (Redis down, DB ok), `"unhealthy"` (DB down).

### Staff Pass Lookup

```
GET /api/v1/lookup/:staff_pass_id
```

```json
{
  "staff_pass_id": "STAFF001",
  "team_name": "Team Alpha",
  "valid": true,
  "mapping_created_at": 1703462400000
}
```

### Eligibility Check

```
GET /api/v1/eligibility/:staff_pass_id
```

```json
{
  "staff_pass_id": "STAFF001",
  "team_name": "Team Alpha",
  "eligible": true,
  "reason": "Team is eligible for redemption"
}
```

### Redeem Present

```
POST /api/v1/redeem/:staff_pass_id
```

```json
{
  "message": "Redemption successful",
  "redemption": {
    "team_name": "Team Alpha",
    "redeemed": true,
    "redeemed_at": "2026-03-07T14:30:45Z"
  }
}
```

### Redemptions CRUD

```
GET    /api/v1/redemptions                # List all
GET    /api/v1/redemptions/:team_name     # Get by team
POST   /api/v1/redemptions                # Create
PUT    /api/v1/redemptions/:team_name     # Update
DELETE /api/v1/redemptions/:team_name     # Delete (also invalidates cache)
```

### Staff Mappings

```
GET /api/v1/staff-mappings                      # List all
GET /api/v1/staff-mappings/:staff_pass_id       # Get by staff pass ID
```

## CSV Data Format

Place `staff_mappings.csv` in the `data/` directory:

```csv
staff_pass_id,team_name,created_at
STAFF_H123804820G,GRYFFINDOR,1623772799000
```

- `staff_pass_id` — unique staff identifier
- `team_name` — team the staff belongs to
- `created_at` — epoch milliseconds (latest mapping per staff pass ID is used)

## Testing

```bash
# All unit tests (8 total)
go test -v ./...

# Just root-level unit tests
go test -v

# Integration tests (requires Docker services running)
docker-compose up postgres redis -d
go test -v -count=1 -tags=integration ./...
```

### Test Coverage

| Package | Tests | What's tested |
|---------|-------|---------------|
| `main` (root) | 8 | CSV parsing (valid/header-only/empty/invalid-timestamp/bad-columns/mixed), env var fallback |
| `integration` | 34 | Real PostgreSQL + Redis via Docker: health check, staff mappings CRUD, lookup, cache prewarm, eligibility, full redemption round-trip, CRUD, 20-goroutine concurrent SETNX, cache-aside population, CSV file loading (valid/missing/empty), route registration, service-level RedeemPresent (success/invalid/already-redeemed), service-level CheckEligibility (eligible/invalid/already-redeemed), invalid JSON handling, update/delete not-found, delete cache invalidation, write-through cache, redemptions list, Redis CacheStore behavioral tests (miss/hit, key isolation, SETNX win/lose, invalidate + re-NX, noop invalidation, ping, 50-goroutine concurrent NX, concurrent writes) |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_NAME` | `govtech_christmas` | Database name |
| `DB_USER` | `postgres` | Database user |
| `DB_PASSWORD` | `password123` | Database password |
| `REDIS_HOST` | `localhost` | Redis host |
| `REDIS_PORT` | `6379` | Redis port |
| `REDIS_PASSWORD` | _(empty)_ | Redis password |
| `REDIS_DB` | `0` | Redis database number |
| `APP_PORT` | `8080` | HTTP server port |