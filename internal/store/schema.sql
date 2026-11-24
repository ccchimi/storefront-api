-- Every statement is idempotent so the schema can be applied on each start.

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY,
    email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    name          TEXT    NOT NULL,
    password_hash TEXT    NOT NULL,
    created_at    TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS products (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    price_cents INTEGER NOT NULL CHECK (price_cents >= 0),
    -- Last line of defence against overselling: the database itself refuses
    -- negative stock even if application code gets it wrong.
    stock       INTEGER NOT NULL CHECK (stock >= 0)
);

CREATE TABLE IF NOT EXISTS orders (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id),
    status      TEXT    NOT NULL CHECK (status IN ('paid')),
    total_cents INTEGER NOT NULL CHECK (total_cents >= 0),
    payment_ref TEXT    NOT NULL,
    created_at  TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS orders_by_user ON orders (user_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS order_items (
    id               INTEGER PRIMARY KEY,
    order_id         INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id       INTEGER NOT NULL REFERENCES products(id),
    -- Name and price are copied so old orders don't change when the catalog does.
    product_name     TEXT    NOT NULL,
    unit_price_cents INTEGER NOT NULL,
    quantity         INTEGER NOT NULL CHECK (quantity > 0),
    line_total_cents INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS order_items_by_order ON order_items (order_id);

CREATE TABLE IF NOT EXISTS invoices (
    id           INTEGER PRIMARY KEY,
    order_id     INTEGER NOT NULL UNIQUE REFERENCES orders(id) ON DELETE CASCADE,
    number       TEXT    NOT NULL UNIQUE,
    amount_cents INTEGER NOT NULL,
    issued_at    TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS emails (
    id         INTEGER PRIMARY KEY,
    order_id   INTEGER REFERENCES orders(id) ON DELETE CASCADE,
    recipient  TEXT    NOT NULL,
    subject    TEXT    NOT NULL,
    body       TEXT    NOT NULL,
    status     TEXT    NOT NULL CHECK (status IN ('queued', 'sent', 'failed')),
    created_at TEXT    NOT NULL,
    sent_at    TEXT
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key             TEXT    NOT NULL,
    request_hash    TEXT    NOT NULL,
    state           TEXT    NOT NULL CHECK (state IN ('processing', 'completed')),
    response_status INTEGER,
    response_body   BLOB,
    created_at      TEXT    NOT NULL,
    PRIMARY KEY (user_id, key)
);
