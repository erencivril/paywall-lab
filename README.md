# paywall-lab

[![ci](https://github.com/erencivril/paywall-lab/actions/workflows/ci.yml/badge.svg)](https://github.com/erencivril/paywall-lab/actions/workflows/ci.yml)

A small Go backend that simulates a subscription paywall, and a test suite that checks it from the API, the analytics events and the browser.

The backend picks a paywall by country and user, takes simulated store purchases and notifications, keeps entitlement state in PostgreSQL, and writes an analytics event for every state change. The tests are the main part of the project.

## Why

Testing subscriptions is mostly testing combinations: country tier, trial or not, renewal, refund, restore, and the analytics event each step should send. Doing that by hand against a real store is slow and hard to repeat. This repo keeps the same rules in a service you can start in seconds, so every rule can be checked automatically and a regression shows up in CI.

Things it checks that tend to go wrong in real apps:

- A country gets the wrong price tier, or the paywall price differs from the price charged.
- A refunded or expired user keeps access.
- The store sends the same notification twice and a renewal is counted twice.
- An analytics event has a missing property, a wrong type, or is sent twice.
- Restore brings back a purchase that was refunded.

## Architecture

```mermaid
flowchart LR
    subgraph tests [tests/]
        PT[pytest: API + contract]
        PW[Playwright E2E]
        TP[tracking-plan.yaml] --> SC[JSON Schemas]
        SC --> PT
    end

    PW --> PAGE[/paywall page/]
    PT --> API

    subgraph app [Go service]
        PAGE --> API[HTTP API]
        API --> SVC[service]
        SVC --> SM[entitlement state machine]
        SVC --> RC[remote config: tiers, prices, variants]
        SVC --> ST[store: pgx + SQL migrations]
    end

    RC --- CFG[(config/remote_config.json)]
    ST --> DB[(PostgreSQL)]
    DB --- EV[analytics_events]
    API -. GET /v1/debug/events .-> PT
```

State changes and their analytics events are written in one database transaction, so an event exists only if the change was committed. The tests read events back through a debug endpoint that is off unless `ENABLE_DEBUG_ENDPOINTS=true`.

## Quickstart

You need Docker. For the tests you also need Python 3.12 and, for unit tests, Go 1.24.

```sh
docker compose up -d --build --wait     # app on http://localhost:8080, PostgreSQL behind it
open http://localhost:8080/paywall?country=US&user_id=demo

make test        # Go unit tests + API and contract tests (starts the stack if needed)
make e2e         # Playwright browser tests
make lint        # golangci-lint
make down        # stop and delete the database volume
```

`make test` and `make e2e` create a virtualenv in `tests/.venv` and install Chromium on first run. Set `APP_PORT` if 8080 is taken. The Go tests that need a database run when `DATABASE_URL` is set and skip otherwise. With the compose stack running: `DATABASE_URL=postgres://paywall:paywall@localhost:5432/paywall go test ./...`.

Try the API by hand:

```sh
curl 'localhost:8080/v1/paywall?country=BR&user_id=demo'

curl -X POST localhost:8080/v1/purchases -H 'content-type: application/json' \
  -d '{"user_id":"demo","product_id":"premium_monthly","store":"app_store","country":"BR","sandbox":true,"start_trial":true}'

curl localhost:8080/v1/entitlements/demo
curl 'localhost:8080/v1/debug/events?user_id=demo'
```

## API

| Method and path | What it does |
|---|---|
| `GET /v1/paywall?country=XX&user_id=...` | Paywall for the country tier and the user's A/B variant. Emits `paywall_viewed`. |
| `POST /v1/purchases` | Simulated store purchase. Optional free trial. Optional `transaction_id` makes retries safe. |
| `POST /v1/restore` | Re-checks a user's purchases on one store. |
| `GET /v1/entitlements/{user_id}` | Current access and subscriptions. |
| `POST /v1/store-notifications` | Simulated server notification: `SUBSCRIBED`, `DID_RENEW`, `EXPIRED`, `REFUND`. Idempotent by `notification_id`. |
| `GET /v1/debug/events` | Analytics events for a user. Debug only. |
| `GET /healthz` | Liveness and database check. |
| `GET /paywall?country=XX&user_id=...` | The HTML paywall page used by the browser tests. |

Errors look like `{"error": {"code": "...", "message": "..."}}`. Codes: `invalid_request` (400), `unknown_transaction` (404), `already_subscribed`, `trial_already_used`, `transaction_conflict`, `invalid_transition` (409), `unknown_product` (422).

Prices come from `config/remote_config.json`, shaped like a Firebase Remote Config value: country groups (`tier1`, `ex_tier1`), prices and trial days per group, and an experiment with a weighted split. A user's variant is a SHA-256 hash of the experiment name and user id, so it never changes between sessions.

## Test strategy

About 290 automated test cases (counting Go subtests). The full reasoning is in [docs/TEST_PLAN.md](docs/TEST_PLAN.md).

| Layer | Where | Count | What it covers |
|---|---|---|---|
| Go unit | `internal/entitlement`, `internal/remoteconfig`, `internal/api` | 74 | State machine table, tier lookup, config validation, variant split, request validation |
| Go with PostgreSQL | `internal/service` | 8 | Full lifecycles, parallel duplicate notifications, rollback of rejected notifications |
| API (pytest) | `tests/api` | 137 | Price per country, purchases, trial rules, restore, notifications, idempotency, validation |
| Contract (pytest) | `tests/contract` | 47 | Every emitted event against JSON Schemas built from `tracking-plan.yaml`, plus checks that the schemas reject bad events |
| E2E (Playwright) | `tests/e2e` | 24 | What the user sees per tier and variant, purchase and restore from the page, error states, phone viewport |

Choices worth knowing about:

- Expected prices and pinned variant assignments are written in the tests, not read from the config. A price change must update the tests on purpose.
- The tracking plan is YAML meant to be read by people. The schemas are generated from it, and a test fails if a plan event is never emitted or an event is emitted that is not in the plan.
- Contract tests also break good events on purpose (string price, wrong tier, extra property, time in seconds) to prove the schemas can fail.
- Tests use a new user per test and never sleep to wait for something. Browser tests use Playwright's auto-waiting assertions.
- CI uploads HTML reports (pytest-html), plus screenshots and traces for failing browser tests.

## CI

[`.github/workflows/ci.yml`](.github/workflows/ci.yml) has three jobs:

1. `go`: `go vet`, `go test -race` against a PostgreSQL service, `golangci-lint`.
2. `mutation`: runs the mutation check below.
3. `e2e`: build the image, `docker compose up --wait`, run API and contract tests, run Playwright tests, upload `reports/` as an artifact.

## Mutation check

Passing tests only matter if they fail when the code is wrong. `scripts/mutation-check.sh` breaks `internal/entitlement/entitlement.go` on purpose in three ways, one at a time, and expects `go test ./internal/entitlement/` to fail each time: a renewal reopens a refunded subscription, a renewal revives an expired subscription, and `HasAccess` ignores the expiry timestamp. The edits are the patch files in `mutation/`. The script prints KILLED or SURVIVED per mutant, restores the file afterwards, and exits non-zero if any mutant survives or a patch no longer applies. Run it locally with `./scripts/mutation-check.sh` (needs Go and a clean working tree for that file). CI runs it as the `mutation` job.

## Project layout

```
cmd/server/            entrypoint: config, graceful shutdown, health check flag
internal/
  api/                 HTTP handlers, validation, middleware (request id, slog)
  service/             business rules, one transaction per state change
  entitlement/         state machine (no I/O) and its table-driven tests
  remoteconfig/        tier lookup, prices, A/B variant
  analytics/           event type and names
  store/               pgx queries and migration runner
migrations/            plain SQL files, embedded in the binary
web/                   paywall page (HTML, JS, CSS), embedded
config/                remote_config.json
tracking-plan.yaml     analytics event definitions
tests/
  api/                 pytest API tests
  contract/            event schema tests and user journeys
  e2e/                 Playwright tests and page object
  support/             client, helpers, price book, schema generator
docs/TEST_PLAN.md      scope, risks, test matrix, manual checks, bug template
Dockerfile             multi-stage build, distroless runtime image
docker-compose.yml     app + PostgreSQL
Makefile               up, down, unit, lint, api, e2e, test
```

## Limits

This is a simulation. It does not verify real store receipts or signed notifications, has no auth, and models one premium entitlement with two products in USD. Details are in the "Known gaps" section of the test plan.

## License

MIT
