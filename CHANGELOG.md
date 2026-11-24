# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This changelog.
- Contributing guide.
- Code of conduct (Contributor Covenant).
- Issue forms for bug reports and feature requests, and a pull request template.
- Security policy.
- Dependabot updates for Go modules, GitHub Actions and the Docker base images.

## [1.0.0] - 2026-11-24

First release.

### Added

- HTTP API for logging in, searching products, managing a cart, checking out and browsing order history.
- SQLite persistence through a pure-Go driver, with the schema applied and demo data seeded on start.
- All-or-nothing checkout: the customer is charged first, then the order, items, stock decrement, invoice and email record are written in a single transaction. A failed write after a successful charge is refunded.
- Per-product locks plus a conditional stock update and a `CHECK (stock >= 0)` constraint, so the last unit is never sold twice.
- `Idempotency-Key` support on checkout: retries replay the original response, keys are scoped per user, persisted across restarts, and released when a request fails.
- Sliding-window rate limits for failed logins (per email), checkouts (per user) and product listings (per IP), answered with `429` and `Retry-After`.
- Strict request validation with a single error format that names every invalid field.
- Confirmation emails sent in the background; shutdown waits for pending deliveries.
- Product listing cache, invalidated after every checkout.
- PBKDF2 password hashing and hashed session tokens.
- `-version` flag, with the version set at build time.
- Integration tests covering concurrency, restarts and failure paths.
- Dockerfile with a static, non-root image.
- CI with formatting, vet, race-detector tests and a coverage badge; a Docker workflow that builds and smoke-tests the image.
- Release binaries for Linux, macOS and Windows with SHA-256 checksums.

[Unreleased]: https://github.com/ccchimi/storefront-api/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/ccchimi/storefront-api/releases/tag/v1.0.0
