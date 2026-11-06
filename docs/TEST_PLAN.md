# Test plan

This plan covers the paywall and subscription flow in this repo: what to test, in what order of importance, and how each part is tested. It is the document I would write before building the automation, then keep up to date as tests are added.

## 1. Scope

In scope:

- Paywall content per country: prices, trial length, copy, A/B variant.
- Purchases (trial and paid), restore, entitlement state.
- Store server notifications: subscribed, renewed, expired, refunded.
- Analytics events sent for each state change: names, properties, counts.
- The web paywall page, as a stand-in for what a user sees in the app.

Out of scope:

- Real store receipts and signature checks. The store is simulated, so nothing here proves the real App Store or Play flow works. See "Manual" below.
- Authentication, rate limiting and load testing. The API has no auth on purpose, so a test can create any user.
- Native UI. The web page only checks the data the app would render.

## 2. Test environments

| Environment | How it is started | Used for |
|---|---|---|
| Local | `make up` (docker compose: app + PostgreSQL) | Everything below |
| CI | GitHub Actions, same compose file | API, contract, E2E on every push |
| Unit only | `go test ./...` | Needs no database, DB-backed tests skip themselves |

Test data is created per test with a fresh user id (`qa-<random>`). Tests never share users and never clean up, because the database is thrown away with `make down`.

## 3. Risks and priorities

Priority is impact times likelihood. P1 gets automated first and runs on every push.

| # | Area | What can go wrong | Impact | Likelihood | Priority |
|---|---|---|---|---|---|
| 1 | Price per country | A country lands in the wrong tier, so users are charged the wrong price or see a price that differs from what is charged | High: revenue loss, store review issues | Medium: config edits, new countries, default-tier logic | P1 |
| 2 | Trial, renewal and refund transitions | A refunded or expired user keeps access, or a paying user loses it | High | Medium: many states and out-of-order notifications | P1 |
| 3 | Event accuracy | Wrong name, missing property, wrong type, duplicate or missing event | High: wrong funnels, wrong revenue numbers, silent for weeks | High: every code change can touch it | P1 |
| 4 | Duplicate notifications | The store retries a notification and a renewal is counted twice | High | High: stores retry by design | P1 |
| 5 | Restore | Restore returns access for refunded or expired purchases, or finds nothing for a valid one | Medium to high | Medium | P1 |
| 6 | A/B variant assignment | Users change variant between sessions, or the split drifts after a code change | Medium: experiment results become unusable | Low to medium | P2 |
| 7 | Free trial abuse | One user gets several trials | Medium | Medium | P2 |
| 8 | Input validation | Bad input causes a 500 or creates bad data | Medium | Medium | P2 |
| 9 | Paywall page rendering | Wrong text, missing price, broken on small screens, console errors | Medium | Medium | P2 |
| 10 | Concurrency | Double tap on buy, parallel identical notifications | Medium | Low | P2 |
| 11 | Observability | No request id, unclear logs | Low | Medium | P3 |

## 4. Test matrix

### 4.1 Price and trial per tier

Expected values are written by hand in `tests/support/price_book.py`. They are not read from the config, so a price change in the config fails the tests until someone confirms it on purpose.

| Tier | Sample countries | Monthly | Yearly | Trial | Automated in |
|---|---|---|---|---|---|
| tier1 | US, CA, GB, AU, DE, FR, NL, SE, CH, JP | $9.99 | $59.99 | 7 days | API, E2E (US, GB, JP) |
| ex_tier1 | BR, IN, TR, MX, ZA, ID, PL, EG | $4.99 | $29.99 | 3 days | API, E2E (BR, IN, TR) |
| default | any code not listed (ZZ) | as ex_tier1 | as ex_tier1 | 3 days | API |

Also covered: lower-case country code, missing or malformed country, and that the price charged at purchase equals the price shown on the paywall.

### 4.2 Subscription state transitions

Rows are the current state, columns are the incoming store notification. Each cell is the expected new state.

| From \ Event | DID_RENEW | EXPIRED | REFUND |
|---|---|---|---|
| trial | active (event `trial_converted`) | expired | refunded |
| active | active (event `subscription_renewed`, count +1) | expired | refunded |
| expired | rejected, 409 | no change | refunded |
| refunded | rejected, 409 | no change | no change |

"No change" means HTTP 200, no state write and no analytics event. "Rejected" means the notification is not remembered, so a retry is processed again. These rules are table-driven unit tests in `internal/entitlement` and are exercised again through the API in `tests/api/test_store_notifications.py`.

### 4.3 Purchase combinations

| Dimension | Values |
|---|---|
| Tier | tier1, ex_tier1 |
| Product | monthly, yearly |
| Trial | yes, no |
| Store | app_store, play |
| Sandbox flag | true, false |

Not every combination is run. Price per tier and product is covered with a full cross product, because that is where money errors happen. Trial, store and sandbox are each covered with at least one pairing, plus the contract tests check store-to-platform mapping on every event.

### 4.4 Edge cases

- Trial: second trial refused, even for a different product; refused trial does not block a normal purchase; trial ends and converts; refund during trial.
- Renewal: first renewal after trial is a conversion, not a renewal; renewal count starts at 1; renewal with an explicit expiry date; renewal that carries an expiry date in the past does not grant access.
- Refund: during trial, while active, after expiry; refund of one product keeps access from another; refund twice.
- Expiry: expire then buy again gives a new transaction; expire twice emits one event.
- Restore: with an active subscription, during a trial, with nothing, with only expired or refunded purchases, on the wrong store, called twice.
- Ordering: renewal after refund; notification for a purchase that has not arrived yet (404, then works on retry).
- Idempotency: same notification id twice, same id for different transactions, same client transaction id twice, same transaction id from another user.
- Events: a rejected or duplicate request emits nothing.

## 5. What is automated and what is manual

| Check | Automated | Manual | Where |
|---|---|---|---|
| Tier prices, trial length, variant per user | Yes | | `tests/api/test_paywall.py` |
| State machine rules | Yes | | `internal/entitlement` (Go) |
| Purchase, restore, entitlement, notification rules | Yes | | `tests/api`, `internal/service` |
| Duplicate notifications, parallel identical notifications | Yes | | `tests/api`, Go service test with 8 goroutines |
| Input validation | Yes | | `tests/api`, `internal/api` |
| Event names, properties, types, order, counts | Yes | | `tests/contract` |
| Schema can actually fail (mutation checks) | Yes | | `tests/contract/test_schema_strictness.py` |
| Paywall text, price, badge, errors, phone viewport | Yes | | `tests/e2e` |
| Real App Store sandbox purchase and accelerated renewals | | Yes | Device with a sandbox account |
| Real Play Store test purchase | | Yes | Internal testing track |
| Store receipt validation and signed notifications | | Yes | Not simulated here |
| Events arriving in the analytics tool with the right properties | | Yes | Check the live debugger after a release candidate |
| Crash reporting on purchase screens | | Yes | Force a test crash, confirm it shows up |
| Visual polish, localisation, accessibility with a screen reader | | Yes | Manual pass per release |
| Airplane mode, app killed during purchase, background and foreground | | Yes | Device |
| Remote config rollout (change a value, see it on a device) | | Yes | Staging project |

Rule of thumb used here: if the answer is a deterministic rule, automate it. If the answer depends on a real store, a real device or human judgement, do it by hand and write down what was checked.

## 6. How the tests are written

- One behaviour per test, named after the behaviour.
- No fixed sleeps. The only wait loop is the one that waits for `/healthz` before the first test. Browser tests use Playwright's auto-waiting assertions.
- Each test creates its own user, so tests can run in any order.
- Contract tests run a set of user journeys once per session, then check every event they produced. A second test checks that each event type in the tracking plan is produced by at least one journey, so the plan cannot drift from the tests.
- The tracking plan (`tracking-plan.yaml`) is the single place to define events. JSON Schemas are generated from it at test time.
- Expected prices and the pinned A/B assignments are written in the tests, not read from the code under test.

## 7. Entry and exit criteria

Entry: the stack starts with `docker compose up --wait` and `/healthz` returns 200.

Exit for a change to merge:

- `go vet`, `go test -race` and the linter pass.
- API, contract and E2E suites pass in CI.
- Any new event is in `tracking-plan.yaml` and has a journey that emits it.
- Any price or tier change updates the price book in the same commit.

## 8. Known gaps and assumptions

- The notification endpoint is not signed. In a real system the signature check is the first thing to test.
- Time passes only through notifications. There is no clock control, so "access ends exactly at expiry" is covered in Go unit tests, not end to end.
- Grace period, billing retry, upgrades and downgrades between plans, family sharing and offer codes are not modelled.
- Out-of-order delivery is handled by the state machine rules above, but the rules themselves are a decision for this project, not a copy of any store's behaviour.
- Currency is USD for both tiers. Local-currency pricing would need more test data.
- Load and soak tests are not included.

## 9. Bug report template

```
Title: <what is wrong, in one line, with the area first>
       e.g. "Restore: refunded yearly plan is returned as restored"

Environment
- Build / commit:
- Stack: local compose | CI | staging
- Store and sandbox flag: app_store | play, sandbox true | false
- Country and tier:
- User id:

Severity: blocker | high | medium | low
Priority:  P1 | P2 | P3

Steps to reproduce
1.
2.
3.

Expected result:
Actual result:

Evidence
- Request and response (status, body, X-Request-Id):
- Events from GET /v1/debug/events?user_id=<id>:
- Screenshot / trace / log lines:

Reproducibility: always | sometimes (x of y runs) | once
Regression: yes (last good build: ...) | no | unknown
Suggested test to add: <which suite and which case>
```
