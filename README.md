# payment-engine

Payment engine in Go modeled after totvs-pay: accounts → workspaces (sellers) → checkouts → bills → transactions, PSP webhooks, and domain events delivered through River jobs.
The PSP sits behind `internal/psp.Provider`. Today the only adapter is `fake`; Stripe comes next.

## Run

```sh
docker compose up -d postgres
go run ./cmd/migrate        # goose schema + River tables
go run ./cmd/api            # :8080
go run ./cmd/worker         # processes River jobs (client notifications)
go test ./...          # needs the compose Postgres; each test gets its own schema
```

After editing `db/queries/*.sql` or migrations, run `go tool sqlc generate`.

## Deploy (VPS)

A shared Caddy proxy (`deploy/proxy`) fronts every project on the VPS. Each project runs its own compose stack on the external `web` network.

1. Point DNS at the VPS: an `A` record `api.payment-engine` → VPS IP.
2. Proxy, once per VPS. Copy `deploy/proxy` to `/srv/proxy`, then fill `.env` from `.env.example` and set the domain in `Caddyfile`:
   ```sh
   docker network create web
   cd /srv/proxy && docker compose up -d
   ```
3. App. Clone the repo into `/srv/payment-engine` and fill `.env` from `.env.example`, then:
   ```sh
   docker compose -f compose.prod.yml up -d --build   # also the redeploy command, after git pull
   ```
4. Call the API with `Authorization: Bearer $PE_API_KEY`. Caddy turns the key into `X-Consumer-Username` (see Auth). The account must list that consumer in `sources`.

Postgres runs in the stack, on the `pgdata` volume, and is never exposed. Back it up off-box, for example from cron:
```sh
docker compose -f compose.prod.yml exec -T postgres pg_dump -U payment payment_engine | gzip > /backups/pe-$(date +%F).sql.gz
```

## Auth

- `X-Consumer-Username` identifies the source of the request (the gateway sets it).
- `X-Account-Id` names the account. That account must list the consumer in `sources`.

## Fake PSP

- `token_id: "tok_decline"` makes the charge decline.
- Credit card charges come back `authorized`. Pix and boleto charges come back `pending`.
- To simulate the PSP, send `POST /dev/fake-psp/events {"type","external_id","code"?}`. The event is signed and goes through the real `/api/v1/webhooks` handler. External ids are `acct_fake_<workspace_id>` and `pi_fake_<bill_id>`.

Agent/contributor rules: [AGENTS.md](AGENTS.md). Architecture: [docs/architecture.md](docs/architecture.md).
