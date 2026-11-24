# Storefront API

[![CI](https://github.com/ccchimi/storefront-api/actions/workflows/ci.yml/badge.svg)](https://github.com/ccchimi/storefront-api/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/ccchimi/storefront-api/badges/coverage.json)](https://github.com/ccchimi/storefront-api/actions/workflows/ci.yml)
[![Go version](https://img.shields.io/github/go-mod/go-version/ccchimi/storefront-api)](go.mod)
[![License: MIT](https://img.shields.io/github/license/ccchimi/storefront-api)](LICENSE)

A small online-store backend in Go: log in, browse products, fill a cart, check out, and look at past orders.

The feature list is short on purpose. The interesting part is what happens when things go wrong: two people buying the last unit at once, a card that gets declined, a client that retries after a timeout, or an email vendor that takes five seconds to answer. Each of those cases is handled explicitly and covered by a test.

Built with the standard library (`net/http`, `database/sql`, `log/slog`) and SQLite through [ncruces/go-sqlite3](https://github.com/ncruces/go-sqlite3), a pure-Go driver, so no C toolchain is needed.

## Quick start

Requires Go 1.27+.

```sh
go run ./cmd/storefront          # listens on :8000, creates storefront.db
go test ./...                    # full test suite
```

The first start seeds 24 products and five users, all with the password `password123`:

`alice@storefront.test`, `bob@storefront.test`, `carol@storefront.test`, `dave@storefront.test`, `erin@storefront.test`

Configuration is through flags or environment variables:

| Flag    | Env       | Default         |
|---------|-----------|-----------------|
| `-addr` | `ADDR`    | `:8000`         |
| `-db`   | `DB_PATH` | `storefront.db` |

With Docker:

```sh
docker build -t storefront-api .
docker run -p 8000:8000 -v storefront-data:/data storefront-api
```

## Walkthrough with curl

A full purchase from a fresh database, with the responses you should see. Start the server with `go run ./cmd/storefront` and run these from a second terminal. The examples use bash and [jq](https://jqlang.org/); on Windows, Git Bash or WSL work as-is.

**1. Log in** and keep the token for the next requests:

```sh
TOKEN=$(curl -s localhost:8000/api/auth/login \
  -d '{"email": "alice@storefront.test", "password": "password123"}' | jq -r .token)
```

```json
{
  "token": "S42K4SW7KAZ4Y7NV2PMZUGXYCZ",
  "expires_at": "2026-11-25T21:36:14Z",
  "user": { "id": 1, "email": "alice@storefront.test", "name": "Alice" }
}
```

**2. Search the catalog.** No token needed:

```sh
curl -s "localhost:8000/api/products?q=bottle"
```

```json
{
  "items": [
    {
      "id": 10, "name": "Water Bottle", "description": "Insulated steel, 750 ml",
      "price_cents": 1999, "price": "19.99", "stock": 100, "in_stock": true
    }
  ]
}
```

**3. Fill the cart.** Each call returns the whole cart:

```sh
curl -s localhost:8000/api/cart/items -H "Authorization: Bearer $TOKEN" \
  -d '{"product_id": 10, "quantity": 3}'

curl -s localhost:8000/api/cart/items -H "Authorization: Bearer $TOKEN" \
  -d '{"product_id": 1, "quantity": 1}'
```

```json
{
  "items": [
    { "product_id": 1, "name": "Mechanical Keyboard", "unit_price_cents": 8999, "quantity": 1, "line_total_cents": 8999, "in_stock": true },
    { "product_id": 10, "name": "Water Bottle", "unit_price_cents": 1999, "quantity": 3, "line_total_cents": 5997, "in_stock": true }
  ],
  "total_cents": 14996,
  "total": "149.96"
}
```

**4. Check out** with an idempotency key, so a retry can never charge twice:

```sh
curl -s -i -X POST localhost:8000/api/checkout -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: 7f9c2b1e-first-order"
```

```http
HTTP/1.1 201 Created
Content-Type: application/json

{
  "id": 1,
  "status": "paid",
  "total_cents": 14996,
  "total": "149.96",
  "item_count": 2,
  "created_at": "2026-11-24T21:36:15.8999261Z",
  "items": [
    { "product_id": 1, "name": "Mechanical Keyboard", "unit_price_cents": 8999, "quantity": 1, "line_total_cents": 8999 },
    { "product_id": 10, "name": "Water Bottle", "unit_price_cents": 1999, "quantity": 3, "line_total_cents": 5997 }
  ],
  "invoice": { "number": "INV-00000001", "amount_cents": 14996, "amount": "149.96", "issued_at": "2026-11-24T21:36:15.8999261Z" }
}
```

A couple of seconds later the server log shows the confirmation email going out: `msg="email sent" to=alice@storefront.test subject="Your order #1 is confirmed"`.

**5. Retry the same request,** as a client would after a timeout. The cart is empty by now, but the key still returns the original order, and nobody is charged again:

```sh
curl -s -i -X POST localhost:8000/api/checkout -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: 7f9c2b1e-first-order"
```

```http
HTTP/1.1 201 Created
Content-Type: application/json
Idempotent-Replayed: true

{"id":1,"status":"paid","total_cents":14996, ... same body as before ...}
```

**6. Look at the order history:**

```sh
curl -s "localhost:8000/api/orders?page=1&limit=10" -H "Authorization: Bearer $TOKEN"
curl -s localhost:8000/api/orders/1 -H "Authorization: Bearer $TOKEN"
```

```json
{
  "items": [
    { "id": 1, "status": "paid", "total_cents": 14996, "total": "149.96", "item_count": 2, "created_at": "2026-11-24T21:36:15.8999261Z" }
  ],
  "page": 1,
  "limit": 10,
  "total": 1
}
```

The second call returns the same order with its items and invoice, as in step 4. Logged in as Bob, `/api/orders/1` is a `404`.

### When things go wrong

**Invalid input** gets a `400` naming every bad field. Here the ID is a string and the quantity a decimal:

```sh
curl -s localhost:8000/api/cart/items -H "Authorization: Bearer $TOKEN" \
  -d '{"product_id": "10", "quantity": 2.5}'
```

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Request validation failed",
    "fields": {
      "product_id": "must be a positive integer",
      "quantity": "must be an integer between 1 and 10"
    }
  }
}
```

**A declined card** gets a `402`, and nothing is written. The simulated provider declines anything over $1,000, so three ergonomic chairs will do it:

```sh
curl -s localhost:8000/api/cart/items -H "Authorization: Bearer $TOKEN" \
  -d '{"product_id": 14, "quantity": 3}'
curl -s -X POST localhost:8000/api/checkout -H "Authorization: Bearer $TOKEN"
```

```json
{ "error": { "code": "PAYMENT_DECLINED", "message": "The payment was declined" } }
```

The cart is left as it was, the order history still shows one order, and the stock is unchanged.

**Too many failed logins** lock the account for the rest of the 15-minute window, even with the right password:

```sh
for i in 1 2 3 4 5; do
  curl -s -o /dev/null localhost:8000/api/auth/login \
    -d '{"email": "bob@storefront.test", "password": "wrong"}'
done
curl -s -i localhost:8000/api/auth/login \
  -d '{"email": "bob@storefront.test", "password": "password123"}'
```

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 899

{"error":{"code":"RATE_LIMITED","message":"Too many requests, slow down"}}
```

## API

Every endpoint except login, products and health needs `Authorization: Bearer <token>`.

| Method   | Path                          | Description                                  |
|----------|-------------------------------|----------------------------------------------|
| `POST`   | `/api/auth/login`             | `{"email","password"}` → `{"token","expires_at","user"}` |
| `POST`   | `/api/auth/logout`            | Ends the current session                     |
| `GET`    | `/api/me`                     | The logged-in user                           |
| `GET`    | `/api/products?q=`            | List or search products (name and description) |
| `GET`    | `/api/cart`                   | Current cart with live prices                |
| `POST`   | `/api/cart/items`             | `{"product_id": 1, "quantity": 2}`           |
| `DELETE` | `/api/cart/items/{product_id}`| Remove a product from the cart               |
| `POST`   | `/api/checkout`               | Place an order for the whole cart. Optional `Idempotency-Key` header |
| `GET`    | `/api/orders?page=&limit=`    | Order history, newest first. `limit` defaults to 10, max 50 |
| `GET`    | `/api/orders/{order_id}`      | One order with its items and invoice         |
| `GET`    | `/healthz`                    | Liveness and database check                  |

Money is always sent as integer cents (`"total_cents": 5997`), with a formatted string next to it for display (`"total": "59.97"`).

Errors share one shape:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Request validation failed",
    "fields": { "quantity": "must be an integer between 1 and 10" }
  }
}
```

| Status | When |
|--------|------|
| `400`  | Invalid input. `fields` names each bad input (`body` if the body isn't a JSON object, `cart` for checkout rules) |
| `401`  | Missing, invalid or expired token; wrong credentials |
| `402`  | Payment declined |
| `404`  | Not found, including orders that belong to someone else |
| `409`  | Not enough stock, or a request with the same idempotency key is still running |
| `422`  | Idempotency key reused for a different cart |
| `429`  | Rate limited; `Retry-After` says how many seconds to wait |

## How the hard parts work

### Checkout is all-or-nothing

1. Validate the cart (not empty, at most 20 distinct products).
2. Lock the products being bought, then check stock and price everything.
3. Charge the payment provider.
4. In one database transaction, write the order, its items, the stock decrements, the invoice and the confirmation email record.
5. Only after the commit: empty the cart, invalidate the product cache, and hand the email to a background sender.

The charge happens before the transaction, not inside it. SQLite allows one writer at a time, and holding the write lock while a slow payment provider thinks would stall every other write in the system. Because nothing is written until the money is secured, a declined card leaves no trace. If the write fails after a successful charge, the charge is refunded.

### No overselling

Two layers protect stock:

- Product locks (`internal/api/keyedmutex.go`) serialize checkouts that share a product, from the stock check until the commit, so two buyers can't both see the last unit and both get charged. Checkouts for unrelated products don't wait on each other. Locks are always taken in ID order, so they can't deadlock.
- The stock decrement is a conditional `UPDATE ... WHERE stock >= ?`, and the schema has `CHECK (stock >= 0)`. Even if the application logic were wrong, the database would refuse to go negative.

### Idempotent checkout

`Idempotency-Key` makes a retry safe:

- The key is reserved in the database with an `INSERT ... ON CONFLICT DO NOTHING` on `(user_id, key)`, so two concurrent requests can't both claim it and keys never collide between users.
- On success the exact response bytes are stored; a retry gets them back unchanged, even though the cart is empty by then.
- A retry while the first request is still running gets `409`. Reusing a key with a different cart gets `422`.
- A failed request (declined card, out of stock) releases the key, so the client can fix the problem and retry with the same key.
- Keys live in SQLite, so they survive restarts. Keys left in "processing" by a crash are cleared on startup.

### Confirmation emails don't block checkout

The email is sent from a goroutine with its own context. The request context is cancelled as soon as the response goes out, and reusing it would kill the send. `App.Close` waits for pending emails before closing the database, and `main` stops the HTTP server first, so a deploy doesn't drop mail. Each email's outcome (`sent` or `failed`) is recorded.

### Money

Amounts are `int64` cents everywhere (`internal/money`). Float arithmetic makes `3 × 19.99` come out as `59.94` once you truncate. Integers can't.

### Rate limiting

A sliding-window limiter (`internal/ratelimit`) with the check and the record under one lock, so 70 simultaneous requests against a limit of 60 give exactly 60 successes.

| Endpoint              | Limit                         | Keyed by      |
|-----------------------|-------------------------------|---------------|
| `POST /api/auth/login`| 5 failed attempts / 15 min    | email         |
| `POST /api/checkout`  | 10 requests / min             | user          |
| `GET /api/products`   | 60 requests / min             | client IP     |

Once an email hits five failures, even the correct password is refused until the window passes; a successful login resets the count.

### Other details

- Passwords are hashed with PBKDF2-SHA256 (600k iterations, per-user salt). Session tokens are random and only their SHA-256 is stored.
- Product search uses bound parameters and escapes `LIKE` wildcards, so `50%` searches for a literal `50%`.
- Request bodies are decoded into `map[string]any` with `UseNumber`, which is what lets validation tell `2`, `"2"`, `2.5`, `true`, `null` and a missing field apart.
- Order history is ordered by `created_at DESC, id DESC`. Timestamps are stored in a fixed-width format so string ordering matches time ordering.
- The product listing cache is invalidated after every checkout. A generation counter stops a slow reader from caching stale data right after an invalidation.
- All state (carts, cache, limiters) lives on the `App`, not in package globals. Tests run many independent Apps side by side.

## Project layout

```
cmd/storefront/      entry point, graceful shutdown
internal/api/        HTTP handlers, middleware, validation, checkout flow
internal/store/      SQLite schema, seed data and queries
internal/auth/       password hashing and session tokens
internal/cart/       in-memory carts
internal/catalog/    product listing cache
internal/money/      integer-cent money type
internal/ratelimit/  sliding-window rate limiter
internal/payment/    payment gateway interface and simulator
internal/email/      email sender interface and log-based sender
```

## Testing

```sh
go test ./...
go test -race ./...
```

The API tests (`internal/api/api_test.go`) start the full app on a real SQLite file in a temp dir, with a fake payment gateway, a fake mailer and a controllable clock. They cover the concurrency cases (overselling, rate limits, in-flight idempotency keys), restarts, and delivery of emails pending at shutdown.

## Trade-offs and what I'd do next

- **Carts are in memory**, so they're lost on restart and tied to a single instance. Moving them to a table is straightforward.
- **Locks and rate limits are per process.** Running several instances would need a shared store (Postgres row locks or Redis) for both.
- **Emails are sent in-process.** A crash between commit and delivery leaves the row as `queued`; an outbox worker that picks up queued rows on startup would make delivery fully reliable.
- **Idempotency keys never expire.** A periodic cleanup of keys older than, say, 24 hours would keep the table small.
- **No product admin endpoints.** The catalog comes from the seed data.

## License

[MIT](LICENSE)
