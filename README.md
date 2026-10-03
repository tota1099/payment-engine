# payment-engine

Payment engine in Go modeled after totvs-pay: accounts → workspaces (sellers) → checkouts → bills → transactions, PSP webhooks, and domain events delivered through River jobs.
The PSP sits behind `internal/psp.Provider`. Today the only adapter is `fake`; Stripe comes next.

## Run

```sh
docker compose up -d postgres
go run ./cmd/migrate        # goose schema + River tables
go run ./cmd/api            # :8080
go run ./cmd/worker         # processes River jobs (client notifications)
go run ./cmd/apikey dev     # prints an API key for source "dev" (see Auth)
go test ./...          # needs the compose Postgres; each test gets its own schema
```

After editing `db/queries/*.sql` or migrations, run `go tool sqlc generate`.

## Deploy (VPS)

A shared Caddy proxy (`deploy/proxy`) fronts every project on the VPS. Each project runs its own compose stack on the external `web` network.

1. Point DNS at the VPS: an `A` record `api.payment-engine` → VPS IP.
2. Proxy, once per VPS. Copy `deploy/proxy` to `/srv/proxy`, then fill `.env` from `.env.example`. The proxy is a copy, so re-copy `Caddyfile` after it changes and reload Caddy:
   ```sh
   docker network create web
   cd /srv/proxy && docker compose up -d
   ```
3. App. Clone the repo into `/srv/payment-engine` and fill `.env` from `.env.example`, then:
   ```sh
   deploy/deploy.sh   # git pull + rebuild; also the redeploy command
   ```
4. Issue an API key (see Auth). The key is printed once:
   ```sh
   docker compose -f compose.prod.yml run --rm --no-deps api /app/apikey my-source
   ```

**Swagger UI:** `https://api.payment-engine.rporto.tech/docs/`, behind basic auth (`DOCS_USER`/`DOCS_PASSWORD_HASH`). Its *Try it out* calls the real API through the bearer route. The spec is `cmd/api/docs/openapi.yaml`, embedded in the binary: update it when a route or DTO changes.

**Auto-deploy:** `.github/workflows/ci.yml` runs vet and tests on every push and PR. On `main`, it then SSHes into the VPS with a key whose `authorized_keys` entry forces `deploy/deploy.sh`. Secrets:
- `VPS_HOST`, `VPS_USER`.
- `VPS_SSH_KEY`: the private key.
- `VPS_KNOWN_HOSTS`: output of `ssh-keyscan <host>`.

Postgres runs in the stack, on the `pgdata` volume, and is never exposed. Back it up off-box, for example from cron:
```sh
docker compose -f compose.prod.yml exec -T postgres pg_dump -U payment payment_engine | gzip > /backups/pe-$(date +%F).sql.gz
```

## Auth

- `Authorization: Bearer <key>` identifies the **source** (consumer) of the request. Keys are issued with `apikey <source>`, stored only as a SHA-256, and revoked with `DELETE FROM api_keys WHERE source = '<source>'`.
- `X-Account-Id` names the account. That account must list the source in `sources`, which happens when the source creates it.

## Fake PSP

- `token_id: "tok_decline"` makes the charge decline.
- Credit card charges come back `authorized`. Pix and boleto charges come back `pending`.
- To simulate the PSP, send `POST /dev/fake-psp/events {"type","external_id","code"?}`. The event is signed and goes through the real `/api/v1/webhooks` handler. External ids are `acct_fake_<workspace_id>` and `pi_fake_<bill_id>`.

Agent/contributor rules: [AGENTS.md](AGENTS.md). Architecture: [docs/architecture.md](docs/architecture.md).
