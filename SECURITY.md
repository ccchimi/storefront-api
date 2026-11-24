# Security Policy

## Supported versions

Security fixes go into the latest release only.

| Version | Supported |
|---------|-----------|
| 1.x     | Yes       |
| < 1.0   | No        |

## Reporting a vulnerability

Please **don't open a public issue** for security problems. Instead, either:

- use GitHub's private reporting: **[Report a vulnerability](https://github.com/ccchimi/storefront-api/security/advisories/new)** (preferred, the report stays private and we can work on the fix together), or
- email **xchimicasla@hotmail.com**.

Either way, include:

- what the problem is and what an attacker could do with it,
- the steps or requests to reproduce it (`curl` commands are ideal),
- the version (`storefront -version`) or commit you tested.

This is a personal project maintained in spare time, so expect a first reply within a week. Once the issue is confirmed, I'll work on a fix, publish a release and credit you in the changelog, unless you'd rather stay anonymous. Please give me a reasonable chance to ship the fix before disclosing anything publicly.

## Scope

In scope: anything in this repository's code, such as authentication and sessions, access to other users' data, SQL injection, ways around the rate limits, or ways to break checkout integrity (being charged without an order, overselling, double charges).

Not vulnerabilities:

- **The demo accounts and their shared password** (`password123`). They're seeded on purpose so the API can be tried right away.
- **The payment and email simulators.** They're stand-ins for real providers and never move money or send mail.
- **Running without TLS.** The server speaks plain HTTP and expects a reverse proxy or load balancer to terminate TLS in front of it.
- **Rate limits not shared across instances.** This is a documented limitation of the in-memory limiter (see the README).

## Security measures in place

For context when reviewing:

- Passwords are hashed with PBKDF2-SHA256 (600,000 iterations, random salt per user) and compared in constant time.
- Session tokens are random, and only their SHA-256 hash is stored, so a database leak doesn't expose live sessions.
- Login failures are limited per email; the same error is returned for an unknown email and a wrong password.
- All SQL uses bound parameters, and `LIKE` wildcards in search terms are escaped.
- Orders belonging to another user return `404`, so their existence isn't revealed.
- Request bodies are capped at 1 MB, and the server has read, write and idle timeouts.
- The Docker image is static, distroless and runs as a non-root user.
