# Ruby Backend

Backend API for the Ruby app, covering authentication, group/vault operations, treasury automation, realtime events, blink flows, and web3 transaction planning.

## Stack

- Language: Go 1.22
- Router: Chi v5
- Database: Postgres + Bun ORM
- Realtime: WebSocket stream (`/api/v1/ws`) — Render and other HTTPS hosts support `wss://` upgrades on the same service URL.
- Helius: see [docs/HELIUS.md](./docs/HELIUS.md) for webhook URL, headers, and payload shapes.
- Auth: Privy token verification + Phantom signed-message fallback
- Web3: Solana RPC + Anchor IDL-aware tx-plan builder

## Local Setup

### Prerequisites

- Go 1.22+
- Docker + Docker Compose
- (Optional for contracts) Solana CLI + Anchor toolchain

### 1) Install and configure

```bash
cd backend
go mod tidy
cp .env.example .env
```

### 2) Start local Postgres

```bash
cd ..
docker compose up -d
# or from backend/
make services-up
```

### 3) Run backend

```bash
cd backend
make run
```

Health check:

```bash
curl http://localhost:8080/health
```

### 4) Stop services

```bash
cd ..
docker compose down
# or from backend/
make services-down
```

## Environment Variables

| Variable | Description |
|---|---|
| `PORT` | HTTP server port |
| `APP_ENV` | Runtime environment (`development`, `production`) |
| `DATABASE_URL` | Postgres DSN |
| `SOLANA_RPC_URL` | Solana RPC endpoint |
| `HELIUS_API_KEY` | Optional webhook auth key for Helius ingestion |
| `SENDAI_API_KEY` | Agent provider key (future/optional integration) |
| `AUTH_JWT_SECRET` | Signing secret for backend session JWTs |
| `AUTH_SESSION_TTL_MINUTES` | Session expiry in minutes |
| `AUTH_DOMAIN` | Domain embedded in Phantom sign-in challenge |
| `BLINK_BASE_URL` | Base URL for generated blink links |
| `PRIVY_APP_ID` | Privy app ID (JWT audience) |
| `PRIVY_ISSUER` | Privy JWT issuer |
| `PRIVY_JWKS_URL` | Privy JWKS endpoint (RS256 tokens) |
| `PRIVY_VERIFICATION_KEY` | PEM EC public key from Privy Dashboard (ES256 access / identity tokens) |
| `AGENT_AUTO_RUN` | Enables periodic treasury agent runs |
| `AGENT_INTERVAL_SECONDS` | Scheduler interval for auto agent runs |
| `RUBY_PROGRAM_ID` | Anchor `ruby_protocol` program ID |
| `SWIG_PROGRAM_ID` | Swig program ID |
| `TOKEN_2022_PROGRAM_ID` | Token-2022 program ID |
| `ANCHOR_IDL_PATH` | Path to generated Anchor IDL JSON |

## Production: database migrations

On startup, `db.Open` runs **`EnsureSchema`** (idempotent): `CREATE TABLE IF NOT EXISTS` for all models, then additive `ALTER TABLE groups ... ADD COLUMN IF NOT EXISTS` for cycle fields.

- If **migrations fail**, the process **exits before listening** — the deployment should fail or remain unhealthy until `DATABASE_URL` and permissions are correct.
- After a successful migrate, the server logs: `database migrations completed successfully`.

**Manual migrate** (same binary as the server — useful for Render SSH or local ops):

```bash
cd backend
DATABASE_URL="postgres://..." ./ruby-server migrate
```

**Readiness**: `GET /health/ready` pings Postgres and returns **503** if the DB is down. In the [Render dashboard](https://dashboard.render.com/), set **Health Check Path** to `/health/ready` if you want instances marked live only when the database answers (optional; default `/health` only checks the process).

## API Overview

Base path: `/api/v1`

### Authentication

Public:
- `POST /auth/phantom/nonce`
- `POST /auth/phantom/verify`
- `POST /auth/privy/verify`

Protected:
- `GET /auth/me`
- `POST /auth/logout`

Most write endpoints require:

`Authorization: Bearer <backend_token>`

### Core Routes by Phase

- Phase 2:
  - `GET /groups`
  - `POST /groups`
  - `POST /groups/{groupID}/contribute`
  - `POST /groups/{groupID}/swig`
  - `GET /groups/{groupID}/swig/proposals`
  - `POST /groups/{groupID}/swig/proposals`
  - `POST /groups/{groupID}/swig/proposals/{proposalID}/approve`
  - `POST /groups/{groupID}/token`
  - `POST /groups/{groupID}/token/validate-transfer`
  - `GET /groups/{groupID}/explorer`

- Phase 3:
  - `GET /agent/yield-rates`
  - `POST /agent/run`
  - `GET /groups/{groupID}/yield`
  - `POST /groups/{groupID}/yield`

- Phase 4 (backend):
  - `GET /ws` (realtime event stream)
  - `POST /webhooks/helius` (validates `X-Helius-Api-Key` if configured)
  - `GET /groups/{groupID}/loans`
  - `POST /groups/{groupID}/loans`
  - `POST /groups/{groupID}/loans/{loanID}/vote`

- Phase 5:
  - `POST /groups/{groupID}/invite-links`
  - `POST /groups/{groupID}/join`
  - `POST /blinks/{blinkType}/create`
  - `POST /blinks/{blinkType}/{actionID}/execute`
  - `GET /blinks/actions`

### Web3 Utility and Tx Planning

- `GET /web3/balance?address=<wallet>`
- `GET /onchain/config`
- `POST /tx/build/{action}`

Supported tx build actions:
- `create-group`
- `contribute`
- `create-loan`
- `vote-loan`
- `create-swig-proposal`
- `approve-swig-proposal`
- `create-blink`

## Database Tables

Primary off-chain mirrors and event state:

- `groups`
- `members`
- `contributions`
- `yield_events`
- `chain_events`
- `auth_sessions`
- `referral_attributions`
- `swig_proposals`
- `swig_proposal_approvals`
- `loan_requests`
- `loan_votes`
- `blink_actions`

Schema is auto-created at server start in `internal/db/db.go`.

## Contracts and Deployment

Contracts live in `../contracts` (Anchor workspace):

- Program: `ruby_protocol`
- Covers on-chain equivalents of group/member, swig proposals, loan voting, blink actions, and yield event recording.

Deploy scripts:

```bash
cd contracts
./scripts/deploy-localnet.sh
# or
./scripts/deploy-devnet.sh
```

After deploy, copy output values (`RUBY_PROGRAM_ID`, `ANCHOR_IDL_PATH`) into `backend/.env`.

## Notes

- Backend currently persists production-facing state in Postgres while exposing web3/tx-plan surfaces for wallet signing and on-chain transition.
- Realtime events are broadcast from major domain actions over `/api/v1/ws`.
