# Security policy

## Reporting a vulnerability

Use GitHub private vulnerability reporting: open the repository's Security tab and choose "Report a vulnerability". If that is not available, open an issue titled "security contact request" without details and the maintainer will reply with a private channel.

Please include the affected endpoint or file, the steps to reproduce, and the commit you tested.

## What to expect

This is a personal portfolio project with one maintainer. Reports are read within a few days. There is no bug bounty.

## Scope

In scope: the Go service, the Dockerfile and compose file, and the workflows in `.github/`. The project is a simulation: it has no real store receipts or user accounts, and several consequences of that are already listed as open threats in [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md). A report that restates an open threat is welcome but is not a new finding.

## Secrets

No real secrets belong in this repository. `dev-only-notification-secret` in `docker-compose.yml` and the CI secret are dummy values. If you find a real credential, report it privately.
