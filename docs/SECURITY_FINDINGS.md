# Security findings log

First scan: run [46003476658](https://github.com/erencivril/paywall-lab/actions/runs/46003476658) on commit `d356d68`. Latest green run: [46004956711](https://github.com/erencivril/paywall-lab/actions/runs/46004956711) on `beea970`. Counts below are what the tools reported on the first run; a "finding" is one alert instance.

| Tool | First-run count | Verdict | Fix commit |
|---|---|---|---|
| CodeQL Go | 4 (`go/log-injection`: `middleware.go` x3, `server.go` x1) | True positive, low severity: `X-Request-Id` and the URL path from the client were logged unsanitised. | `509f567` |
| CodeQL Python | 0 | | |
| Semgrep OSS | 30 | 25 true positives of supply-chain hygiene, no app bugs: 25 `github-actions-mutable-action-tag` (8 in `ci.yml`, 17 in `security.yml`), 1 `missing-user-entrypoint` (Dockerfile), 4 `dependabot-missing-cooldown`. All fixed. | `509f567` |
| gitleaks (16 commits) | 0 | No leaks. | |
| govulncheck | 3 reachable vulnerabilities in 2 modules: GO-2026-6629 and GO-2026-5970 (`golang.org/x/text` 0.24.0), GO-2026-5004 (`pgx/v5` 5.7.6) | True positives. Found by running the same version of the tool on the commit before the fix; the CI job itself failed first on a tool install error (see note). | `509f567` (pgx 5.13.0, x/text 0.44.0) |
| pip-audit (first ran in run [46004223060](https://github.com/erencivril/paywall-lab/actions/runs/46004223060), because the SCA job stopped at the govulncheck install step on the first run) | 2 entries for 1 package: pytest 8.4.2, PYSEC-2026-1845 (fixed in 9.0.3) | True positive in a test-only dependency. Fixed by requiring `pytest>=9.0.3`. | `beea970` |
| Trivy (image) | 37 HIGH or CRITICAL with a fix (2 CRITICAL, 35 HIGH) in the Go binary (Go standard library of the 1.24 builder image, pgx, x/crypto, x/text), plus 35 lower-severity or unrated entries in the SARIF | True positives. Fixed by moving the builder image and toolchain to Go 1.27.4 and updating modules. The 4 unrated Debian `tzdata` and library notices are no longer reported once the SARIF is limited to the gate severities. | `509f567`, `beea970` |
| ZAP API scan | 11 alerts, 0 High: 1 real (500 from `GET /v1/debug/events?user_id=%00`), 1 header (`Cross-Origin-Resource-Policy` missing), rest Low or Informational | Real: the NUL byte reached PostgreSQL and returned 500. Fixed with input validation and tests. Header added. The Low alerts that remain are on the scan helper server (`python -m http.server` on port 8000 serving `openapi.yaml`) or on the HTML page, and are listed below. | `509f567` |

## Real defects found by the scans and the manual review

1. Dependencies with known vulnerabilities (govulncheck, Trivy, pip-audit). Fixed.
2. `GET /v1/debug/events` returned 500 for a malformed `user_id`. Fixed, tests in `middleware_test.go` and `test_security_headers.py`.
3. Log injection through `X-Request-Id`. Fixed, tests in `middleware_test.go`.
4. Manual review, not a scanner: `POST /v1/store-notifications` accepted unsigned requests (T01). Fixed in `cc589cd`, tests in `signature_test.go` and `test_notification_authenticity.py`. None of the scanners reported it.

## Open items from manual review

T05 and T07 to T09, T11 to T13, T17 in [THREAT_MODEL.md](THREAT_MODEL.md). They are not scanner findings and are not fixed. T08 and T09 (no authentication on purchases, restore, entitlements) are the largest.

## False positives and accepted results

| Tool | Result | Verdict | Reason |
|---|---|---|---|
| ZAP | Timestamp Disclosure - Unix (10096) | False positive | It matches the example `1700000000` in `docs/openapi.yaml`, served by the scan helper, not by the app. Listed in `.zap/rules.tsv`. |
| ZAP | X-Content-Type-Options, `Server` header, CORP on `localhost:8000` | Not the application | Those responses come from Python's `http.server` that only serves the OpenAPI file during the scan. The same headers on the app are set and tested. |
| ZAP | Unexpected Content-Type (100001) | Accepted | The spider reaches the HTML paywall page, which is not in the JSON API description. Listed in `.zap/rules.tsv`. |
| ZAP | Client Error response code (404, 400) | Informational | Fuzzed requests are expected to be rejected. |
| ZAP | Sensitive Information in URL (`user_id`) | Accepted | `user_id` is an opaque test identifier in a simulation. In production the user is taken from an authenticated session (T09). |

Note on the first CI run of `govulncheck`: the job failed with `golang.org/x/vuln@v1.10.0 requires go >= 1.26.0 (running go 1.24.13)`, so it reported nothing. After pinning `govulncheck@v1.10.0` and updating the Go toolchain it reported `No vulnerabilities found.` The three findings above come from running v1.10.0 on commit `d356d68`.
