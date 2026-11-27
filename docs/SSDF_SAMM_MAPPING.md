# Mapping to NIST SSDF and OWASP SAMM

This maps controls that exist in this repository to practices in NIST SP 800-218 (SSDF 1.1) and OWASP SAMM 2. It is a self-assessment of a small project, not a certification. A practice that has no control here is listed as not covered.

## NIST SSDF

| SSDF practice | Control in this repository |
|---|---|
| PO.1 Define security requirements | `docs/THREAT_MODEL.md` (threats and mitigations), `SECURITY.md` (scope and reporting) |
| PO.3 Implement supporting toolchains | `.github/workflows/security.yml` and `ci.yml`, golangci-lint, pinned GitHub Actions (commit SHAs), Dependabot for gomod, pip, github-actions, docker |
| PO.5 Secure build environments | Workflows use `permissions: contents: read`, with `security-events: write` only on jobs that upload SARIF; CI uses a dummy signing secret |
| PS.1 Protect code from unauthorised change and tampering | gitleaks over full history, GitHub secret scanning with push protection, no secrets in the repository, action pinning |
| PS.2 Provide a mechanism to verify release integrity | Not covered: images are built in CI but not signed and no SBOM is published |
| PW.1 Design software to meet security requirements | STRIDE model; notification signature design in `THREAT_MODEL.md` |
| PW.2 Review the design | The threat model is the design review; it is written by the code author, not by a second reviewer |
| PW.4 Reuse existing secure software | Standard library `crypto/hmac`; pgx with parameterised queries; distroless base image |
| PW.5 Create source code following secure practices | Input validation (`validate.go`), unknown JSON fields rejected, constant-time signature compare, log sanitising, security headers |
| PW.6 Configure the build to improve security | Static CGO-free binary, non-root `USER`, distroless image |
| PW.7 Review and analyse code | CodeQL (Go, Python, `security-extended`), Semgrep (`p/default`, `p/golang`, `p/python`), `go vet`, golangci-lint |
| PW.8 Test executable code | Go tests (unit and PostgreSQL-backed), pytest API and contract tests, Playwright E2E, mutation job that injects 3 state-machine bugs, OWASP ZAP API scan |
| PW.9 Secure default settings | Server refuses to start without a signing secret; debug endpoints off by default |
| RV.1 Identify and confirm vulnerabilities | govulncheck, pip-audit, Trivy, Dependabot, scheduled weekly run, `SECURITY.md` reporting channel |
| RV.2 Assess, prioritise and remediate | `docs/SECURITY_FINDINGS.md` (verdict and fix commit per finding), threat likelihood and impact in `THREAT_MODEL.md` |
| RV.3 Analyse vulnerabilities for root causes | Partly: findings log records cause; no formal root-cause review |

## OWASP SAMM

| SAMM practice | Level reached and control |
|---|---|
| Design: Threat Assessment | Level 1: a STRIDE threat model exists with data flows, trust boundaries and statuses. Not level 2: no review by a second person, no abuse-case backlog |
| Design: Security Requirements | Partial: requirements are the mitigations in the threat model; no separate requirements document |
| Implementation: Secure Build | Level 1 to 2: build is scripted in CI, actions pinned to SHAs, dependencies updated by Dependabot. No signed artifacts or SBOM |
| Implementation: Secure Deployment | Not covered beyond a non-root image and a dev-only compose file |
| Implementation: Defect Management | Level 1: findings are recorded with verdicts and fix commits; no severity SLA |
| Verification: Security Testing | Level 1 to 2: SAST, SCA, secret scanning, container scan and DAST run automatically on push, PR and weekly, with code-scanning results |
| Verification: Requirements-driven Testing | Level 1: tests assert the threat-model mitigations (signature rejection, replay, tampering, refund then renewal, entitlement revocation); mutation job checks that tests detect state-machine bugs |
| Verification: Architecture Assessment | Level 1: the threat model lists open design gaps (no authentication, self-reported country) |
| Governance and Operations | Not covered |
