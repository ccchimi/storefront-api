package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- auth --------------------------------------------------------------

func TestLogin(t *testing.T) {
	e := newEnv(t)

	token := e.login(alice)
	me := e.do("GET", "/api/me", token, nil)
	expectStatus(t, me, http.StatusOK)
	if me.body["email"] != alice {
		t.Fatalf("me = %v", me.body)
	}

	wrong := e.do("POST", "/api/auth/login", "", map[string]any{"email": alice, "password": "nope"})
	expectStatus(t, wrong, http.StatusUnauthorized)

	expectStatus(t, e.do("GET", "/api/cart", "", nil), http.StatusUnauthorized)
	expectStatus(t, e.do("GET", "/api/cart", "not-a-token", nil), http.StatusUnauthorized)
}

func TestLogoutEndsSession(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	expectStatus(t, e.do("POST", "/api/auth/logout", token, nil), http.StatusNoContent)
	expectStatus(t, e.do("GET", "/api/me", token, nil), http.StatusUnauthorized)
}

func TestLoginValidation(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name  string
		body  any
		field string
	}{
		{"not json", `{"email":`, "body"},
		{"array body", `[1,2]`, "body"},
		{"missing email", map[string]any{"password": "x"}, "email"},
		{"null email", map[string]any{"email": nil, "password": "x"}, "email"},
		{"bad email", map[string]any{"email": "alice", "password": "x"}, "email"},
		{"email with name", map[string]any{"email": "A <a@b.co>", "password": "x"}, "email"},
		{"empty password", map[string]any{"email": alice, "password": ""}, "password"},
		{"numeric password", map[string]any{"email": alice, "password": 123}, "password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := e.do("POST", "/api/auth/login", "", tc.body)
			expectStatus(t, res, http.StatusBadRequest)
			if res.errorCode() != "VALIDATION_ERROR" || res.fields()[tc.field] == nil {
				t.Fatalf("want validation error on %q, got %s", tc.field, res.raw)
			}
		})
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	bad := map[string]any{"email": alice, "password": "wrong"}
	good := map[string]any{"email": alice, "password": "password123"}

	for range 5 {
		expectStatus(t, e.do("POST", "/api/auth/login", "", bad), http.StatusUnauthorized)
	}

	// Locked out, even with the right password.
	res := e.do("POST", "/api/auth/login", "", good)
	expectStatus(t, res, http.StatusTooManyRequests)
	if res.header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}

	// Other accounts are unaffected.
	e.login(bob)

	e.clock.Advance(15 * time.Minute)
	e.login(alice)
}

func TestSuccessfulLoginResetsFailures(t *testing.T) {
	e := newEnv(t)
	bad := map[string]any{"email": alice, "password": "wrong"}

	for range 4 {
		e.do("POST", "/api/auth/login", "", bad)
	}
	e.login(alice)
	for range 4 {
		expectStatus(t, e.do("POST", "/api/auth/login", "", bad), http.StatusUnauthorized)
	}
	e.login(alice)
}

// --- products ----------------------------------------------------------

func TestProductSearch(t *testing.T) {
	e := newEnv(t)

	all := e.do("GET", "/api/products", "", nil)
	expectStatus(t, all, http.StatusOK)
	if n := len(all.body["items"].([]any)); n != 24 {
		t.Fatalf("listed %d products, want 24", n)
	}

	res := e.do("GET", "/api/products?q=MECHANICAL", "", nil)
	items := res.body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["name"] != "Mechanical Keyboard" {
		t.Fatalf("search returned %s", res.raw)
	}

	// Descriptions are searched too: the wrist rest is "keyboard size".
	if n := len(e.do("GET", "/api/products?q=keyboard", "", nil).body["items"].([]any)); n != 2 {
		t.Fatalf("q=keyboard matched %d products, want 2", n)
	}
}

func TestProductSearchIsNotInjectable(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{"' OR 1=1 --", "%' OR '1'='1", `x"; DROP TABLE products; --`, "%"} {
		res := e.do("GET", "/api/products?q="+urlQuery(q), "", nil)
		expectStatus(t, res, http.StatusOK)
		if n := len(res.body["items"].([]any)); n != 0 {
			t.Fatalf("q=%q matched %d products", q, n)
		}
	}
	if e.count(`SELECT COUNT(*) FROM products`) != 24 {
		t.Fatal("products table was modified")
	}
}

func TestProductQueryTooLong(t *testing.T) {
	e := newEnv(t)
	expectStatus(t, e.do("GET", "/api/products?q="+strings.Repeat("a", 100), "", nil), http.StatusOK)
	res := e.do("GET", "/api/products?q="+strings.Repeat("a", 101), "", nil)
	expectStatus(t, res, http.StatusBadRequest)
	if res.fields()["q"] == nil {
		t.Fatalf("want error on q, got %s", res.raw)
	}
}

func TestProductsRateLimitIsExactUnderLoad(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	codes := map[int]int{}
	var wg sync.WaitGroup
	for range 70 {
		wg.Go(func() {
			res := e.do("GET", "/api/products", "", nil)
			mu.Lock()
			codes[res.status]++
			mu.Unlock()
		})
	}
	wg.Wait()
	if codes[http.StatusOK] != 60 || codes[http.StatusTooManyRequests] != 10 {
		t.Fatalf("status counts = %v, want 60 OK and 10 rate limited", codes)
	}
}

// --- cart --------------------------------------------------------------

func TestCartsAreIsolatedPerUser(t *testing.T) {
	e := newEnv(t)
	a, b := e.login(alice), e.login(bob)

	e.addToCart(a, keyboard, 1)
	e.addToCart(a, waterBottle, 2)

	if n := e.cartSize(b); n != 0 {
		t.Fatalf("bob sees %d items he never added", n)
	}
	e.addToCart(b, hoodie, 1)
	if n := e.cartSize(a); n != 2 {
		t.Fatalf("alice's cart has %d lines, want 2", n)
	}
	if n := e.cartSize(e.login(carol)); n != 0 {
		t.Fatalf("first-time user has %d items in cart", n)
	}
}

func TestRemoveFromCart(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	e.addToCart(token, keyboard, 1)

	expectStatus(t, e.do("DELETE", fmt.Sprintf("/api/cart/items/%d", keyboard), token, nil), http.StatusOK)
	expectStatus(t, e.do("DELETE", fmt.Sprintf("/api/cart/items/%d", keyboard), token, nil), http.StatusNotFound)
	expectStatus(t, e.do("DELETE", "/api/cart/items/abc", token, nil), http.StatusBadRequest)
}

func TestAddToCartValidation(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	cases := []struct {
		name  string
		body  any
		field string
	}{
		{"invalid json", `{"product_id": 1,`, "body"},
		{"json string", `"hello"`, "body"},
		{"missing product", map[string]any{"quantity": 1}, "product_id"},
		{"null product", map[string]any{"product_id": nil, "quantity": 1}, "product_id"},
		{"string product", map[string]any{"product_id": "1", "quantity": 1}, "product_id"},
		{"decimal product", map[string]any{"product_id": 1.5, "quantity": 1}, "product_id"},
		{"zero product", map[string]any{"product_id": 0, "quantity": 1}, "product_id"},
		{"unknown product", map[string]any{"product_id": 9999, "quantity": 1}, "product_id"},
		{"missing quantity", map[string]any{"product_id": 1}, "quantity"},
		{"string quantity", map[string]any{"product_id": 1, "quantity": "2"}, "quantity"},
		{"decimal quantity", `{"product_id": 1, "quantity": 2.5}`, "quantity"},
		{"float-looking integer", `{"product_id": 1, "quantity": 2.0}`, "quantity"},
		{"bool quantity", map[string]any{"product_id": 1, "quantity": true}, "quantity"},
		{"zero quantity", map[string]any{"product_id": 1, "quantity": 0}, "quantity"},
		{"quantity above 10", map[string]any{"product_id": 1, "quantity": 11}, "quantity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := e.do("POST", "/api/cart/items", token, tc.body)
			expectStatus(t, res, http.StatusBadRequest)
			if res.fields()[tc.field] == nil {
				t.Fatalf("want error on %q, got %s", tc.field, res.raw)
			}
		})
	}
	if n := e.cartSize(token); n != 0 {
		t.Fatalf("invalid requests changed the cart (%d lines)", n)
	}
}

// --- checkout ----------------------------------------------------------

func TestCheckoutHappyPath(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	e.addToCart(token, waterBottle, 3)
	e.addToCart(token, keyboard, 1)

	res := e.checkout(token)
	expectStatus(t, res, http.StatusCreated)

	// 3 x 19.99 + 89.99 = 149.96, to the cent.
	if res.body["total_cents"] != float64(14996) || res.body["total"] != "149.96" {
		t.Fatalf("total = %v / %v, want 14996 / 149.96", res.body["total_cents"], res.body["total"])
	}
	invoice := res.body["invoice"].(map[string]any)
	if invoice["amount_cents"] != float64(14996) {
		t.Fatalf("invoice amount = %v", invoice["amount_cents"])
	}
	if res.body["status"] != "paid" {
		t.Fatalf("status = %v", res.body["status"])
	}

	if e.stock(waterBottle) != 97 || e.stock(keyboard) != 24 {
		t.Fatalf("stock not decremented: bottle %d, keyboard %d", e.stock(waterBottle), e.stock(keyboard))
	}
	if e.count(`SELECT COUNT(*) FROM order_items`) != 2 || e.count(`SELECT COUNT(*) FROM invoices`) != 1 ||
		e.count(`SELECT COUNT(*) FROM emails`) != 1 {
		t.Fatal("order items, invoice or email record missing")
	}
	if n := e.cartSize(token); n != 0 {
		t.Fatalf("cart still has %d lines after checkout", n)
	}
	if e.pay.chargeCount() != 1 {
		t.Fatalf("charged %d times", e.pay.chargeCount())
	}

	e.stop() // waits for the confirmation email
	if sent := e.mail.delivered(); len(sent) != 1 || sent[0].To != alice {
		t.Fatalf("delivered emails = %+v", sent)
	}
	if e.count(`SELECT COUNT(*) FROM emails WHERE status = 'sent'`) != 1 {
		t.Fatal("email record not marked as sent")
	}
}

func TestCheckoutEmptyCart(t *testing.T) {
	e := newEnv(t)
	res := e.checkout(e.login(alice))
	expectStatus(t, res, http.StatusBadRequest)
	if res.fields()["cart"] == nil {
		t.Fatalf("want error on cart, got %s", res.raw)
	}
}

func TestCheckoutRejectsMoreThan20DistinctProducts(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	for id := 1; id <= 21; id++ {
		e.addToCart(token, id, 1) // adding is never limited
	}
	res := e.checkout(token)
	expectStatus(t, res, http.StatusBadRequest)
	if res.fields()["cart"] == nil {
		t.Fatalf("want error on cart, got %s", res.raw)
	}
	if e.pay.chargeCount() != 0 {
		t.Fatal("payment provider was called for an invalid request")
	}

	expectStatus(t, e.do("DELETE", "/api/cart/items/21", token, nil), http.StatusOK)
	expectStatus(t, e.checkout(token), http.StatusCreated)
}

func TestCheckoutIsAllOrNothing(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	e.addToCart(token, keyboard, 2)
	e.addToCart(token, hoodie, 1)
	e.setStock(hoodie, 0)

	res := e.checkout(token)
	expectStatus(t, res, http.StatusConflict)
	if res.errorCode() != "OUT_OF_STOCK" {
		t.Fatalf("code = %s", res.errorCode())
	}

	if e.pay.chargeCount() != 0 {
		t.Fatal("customer was charged for a failed checkout")
	}
	if e.stock(keyboard) != 25 {
		t.Fatalf("keyboard stock changed to %d", e.stock(keyboard))
	}
	for _, table := range []string{"orders", "order_items", "invoices", "emails"} {
		if n := e.count(`SELECT COUNT(*) FROM ` + table); n != 0 {
			t.Fatalf("%s has %d rows after a failed checkout", table, n)
		}
	}
	if n := e.cartSize(token); n != 2 {
		t.Fatalf("cart has %d lines, want it untouched (2)", n)
	}
	e.stop()
	if len(e.mail.delivered()) != 0 {
		t.Fatal("confirmation email sent for a failed checkout")
	}
}

func TestDeclinedPaymentLeavesNoTrace(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	e.addToCart(token, keyboard, 1)
	e.pay.decline.Store(true)

	res := e.checkout(token)
	expectStatus(t, res, http.StatusPaymentRequired)

	for _, table := range []string{"orders", "order_items", "invoices", "emails"} {
		if n := e.count(`SELECT COUNT(*) FROM ` + table); n != 0 {
			t.Fatalf("%s has %d rows after a declined payment", table, n)
		}
	}
	if e.stock(keyboard) != 25 {
		t.Fatal("stock changed after a declined payment")
	}
	orders := e.do("GET", "/api/orders", token, nil)
	if orders.body["total"] != float64(0) {
		t.Fatalf("order history shows %v orders", orders.body["total"])
	}
	if n := e.cartSize(token); n != 1 {
		t.Fatal("cart was emptied after a declined payment")
	}
}

func TestNoOversellingUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	e.setStock(keyboard, 3)

	users := []string{alice, bob, carol, dave, erin}
	tokens := make([]string, len(users))
	for i, u := range users {
		tokens[i] = e.login(u)
		e.addToCart(tokens[i], keyboard, 1)
	}
	e.pay.latency.Store(int64(20 * time.Millisecond)) // widen the race window

	var mu sync.Mutex
	codes := map[int]int{}
	var wg sync.WaitGroup
	for _, tok := range tokens {
		wg.Go(func() {
			res := e.checkout(tok)
			mu.Lock()
			codes[res.status]++
			mu.Unlock()
		})
	}
	wg.Wait()

	if codes[http.StatusCreated] != 3 || codes[http.StatusConflict] != 2 {
		t.Fatalf("status counts = %v, want 3 created and 2 conflicts", codes)
	}
	if s := e.stock(keyboard); s != 0 {
		t.Fatalf("stock = %d, want 0", s)
	}
	if e.pay.chargeCount() != 3 {
		t.Fatalf("charged %d times, want 3", e.pay.chargeCount())
	}
}

func TestListingsReflectStockAfterCheckout(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)

	before := e.do("GET", "/api/products?q=hoodie", "", nil) // warms the cache
	if hoodieStock(before) != float64(1) {
		t.Fatalf("hoodie stock = %v before checkout", hoodieStock(before))
	}
	e.do("GET", "/api/products", "", nil)

	e.addToCart(token, hoodie, 1)
	expectStatus(t, e.checkout(token), http.StatusCreated)

	if s := hoodieStock(e.do("GET", "/api/products?q=hoodie", "", nil)); s != float64(0) {
		t.Fatalf("search still shows stock %v", s)
	}
	for _, it := range e.do("GET", "/api/products", "", nil).body["items"].([]any) {
		p := it.(map[string]any)
		if p["id"] == float64(hoodie) && (p["stock"] != float64(0) || p["in_stock"] != false) {
			t.Fatalf("listing still shows %v", p)
		}
	}
}

func hoodieStock(res result) any {
	for _, it := range res.body["items"].([]any) {
		p := it.(map[string]any)
		if p["id"] == float64(hoodie) {
			return p["stock"]
		}
	}
	return nil
}

func TestCheckoutRateLimitIsPerUser(t *testing.T) {
	e := newEnv(t)
	a, b := e.login(alice), e.login(bob)

	for range 10 {
		expectStatus(t, e.checkout(a), http.StatusBadRequest) // empty cart, still counts
	}
	res := e.checkout(a)
	expectStatus(t, res, http.StatusTooManyRequests)
	if res.header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
	expectStatus(t, e.checkout(b), http.StatusBadRequest)

	e.clock.Advance(time.Minute)
	expectStatus(t, e.checkout(a), http.StatusBadRequest)
}

func TestCheckoutDoesNotWaitForEmail(t *testing.T) {
	e := newEnv(t)
	e.mail.latency = 1500 * time.Millisecond
	token := e.login(alice)
	e.addToCart(token, keyboard, 1)

	start := time.Now()
	expectStatus(t, e.checkout(token), http.StatusCreated)
	if took := time.Since(start); took > time.Second {
		t.Fatalf("checkout took %v; it should not wait for the email vendor", took)
	}
	if len(e.mail.delivered()) != 0 {
		t.Fatal("email delivered suspiciously fast")
	}

	e.stop() // Close must wait for the pending email
	if len(e.mail.delivered()) != 1 {
		t.Fatal("pending confirmation email was dropped on shutdown")
	}
}

// --- idempotency -------------------------------------------------------

func TestIdempotentRetryReplaysOriginalResponse(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	e.addToCart(token, keyboard, 1)

	first := e.checkout(token, "Idempotency-Key", "order-1")
	expectStatus(t, first, http.StatusCreated)

	// The cart is empty now, but the retry still gets the original answer.
	second := e.checkout(token, "Idempotency-Key", "order-1")
	if second.status != first.status || string(second.raw) != string(first.raw) {
		t.Fatalf("replay differs:\n first: %d %s\nsecond: %d %s", first.status, first.raw, second.status, second.raw)
	}
	if e.pay.chargeCount() != 1 || e.count(`SELECT COUNT(*) FROM orders`) != 1 {
		t.Fatal("retry created a second order or charge")
	}
}

func TestIdempotencyKeyReusedForDifferentCart(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	e.addToCart(token, keyboard, 1)
	expectStatus(t, e.checkout(token, "Idempotency-Key", "k"), http.StatusCreated)

	e.addToCart(token, waterBottle, 2)
	res := e.checkout(token, "Idempotency-Key", "k")
	expectStatus(t, res, http.StatusUnprocessableEntity)
	if e.pay.chargeCount() != 1 {
		t.Fatal("mismatched key triggered a charge")
	}
}

func TestIdempotencyKeyInFlight(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	e.addToCart(token, keyboard, 1)
	e.pay.latency.Store(int64(500 * time.Millisecond))

	firstDone := make(chan result, 1)
	go func() { firstDone <- e.checkout(token, "Idempotency-Key", "slow") }()
	time.Sleep(150 * time.Millisecond) // let the first request reserve the key

	expectStatus(t, e.checkout(token, "Idempotency-Key", "slow"), http.StatusConflict)
	expectStatus(t, <-firstDone, http.StatusCreated)
	if e.pay.chargeCount() != 1 {
		t.Fatalf("charged %d times", e.pay.chargeCount())
	}
}

func TestFailedRequestReleasesIdempotencyKey(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	e.addToCart(token, keyboard, 1)

	e.pay.decline.Store(true)
	expectStatus(t, e.checkout(token, "Idempotency-Key", "retry-me"), http.StatusPaymentRequired)

	e.pay.decline.Store(false)
	expectStatus(t, e.checkout(token, "Idempotency-Key", "retry-me"), http.StatusCreated)
}

func TestIdempotencyKeysAreScopedPerUser(t *testing.T) {
	e := newEnv(t)
	a, b := e.login(alice), e.login(bob)
	e.addToCart(a, keyboard, 1)
	e.addToCart(b, waterBottle, 1)

	ra := e.checkout(a, "Idempotency-Key", "shared")
	rb := e.checkout(b, "Idempotency-Key", "shared")
	expectStatus(t, ra, http.StatusCreated)
	expectStatus(t, rb, http.StatusCreated)
	if ra.body["id"] == rb.body["id"] {
		t.Fatal("two users sharing a key got the same order")
	}
}

func TestIdempotencyKeysSurviveRestart(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	e.addToCart(token, keyboard, 1)
	first := e.checkout(token, "Idempotency-Key", "durable")
	expectStatus(t, first, http.StatusCreated)

	e.restart()

	again := e.checkout(token, "Idempotency-Key", "durable")
	if again.status != first.status || string(again.raw) != string(first.raw) {
		t.Fatalf("replay after restart differs: %d %s", again.status, again.raw)
	}
	if e.pay.chargeCount() != 1 {
		t.Fatal("restart let the same key charge again")
	}
}

// --- orders ------------------------------------------------------------

func TestOrderHistoryPagination(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)

	var ids []float64
	for range 3 {
		e.addToCart(token, waterBottle, 1)
		res := e.checkout(token)
		expectStatus(t, res, http.StatusCreated)
		ids = append(ids, res.body["id"].(float64))
		e.clock.Advance(time.Second)
	}

	page1 := e.do("GET", "/api/orders?page=1&limit=2", token, nil)
	expectStatus(t, page1, http.StatusOK)
	items := page1.body["items"].([]any)
	if len(items) != 2 || page1.body["total"] != float64(3) || page1.body["page"] != float64(1) || page1.body["limit"] != float64(2) {
		t.Fatalf("page 1 = %s", page1.raw)
	}
	if items[0].(map[string]any)["id"] != ids[2] {
		t.Fatalf("page 1 should start with the newest order %v, got %s", ids[2], page1.raw)
	}

	page2 := e.do("GET", "/api/orders?page=2&limit=2", token, nil)
	if items := page2.body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != ids[0] {
		t.Fatalf("page 2 = %s", page2.raw)
	}

	defaults := e.do("GET", "/api/orders", token, nil)
	if defaults.body["limit"] != float64(10) || defaults.body["page"] != float64(1) {
		t.Fatalf("defaults = %s", defaults.raw)
	}
	expectStatus(t, e.do("GET", "/api/orders?limit=50", token, nil), http.StatusOK)
}

func TestOrderHistoryValidation(t *testing.T) {
	e := newEnv(t)
	token := e.login(alice)
	for _, q := range []string{"limit=51", "limit=0", "limit=abc", "page=0", "page=-1", "page=1.5", "page="} {
		res := e.do("GET", "/api/orders?"+q, token, nil)
		expectStatus(t, res, http.StatusBadRequest)
		if res.errorCode() != "VALIDATION_ERROR" {
			t.Fatalf("%s: %s", q, res.raw)
		}
	}
}

func TestUsersCannotSeeOtherUsersOrders(t *testing.T) {
	e := newEnv(t)
	a, b := e.login(alice), e.login(bob)
	e.addToCart(a, keyboard, 1)
	order := e.checkout(a)
	path := fmt.Sprintf("/api/orders/%v", order.body["id"])

	own := e.do("GET", path, a, nil)
	expectStatus(t, own, http.StatusOK)
	if len(own.body["items"].([]any)) != 1 || own.body["invoice"] == nil {
		t.Fatalf("order detail = %s", own.raw)
	}
	expectStatus(t, e.do("GET", path, b, nil), http.StatusNotFound)
	expectStatus(t, e.do("GET", "/api/orders/99999", a, nil), http.StatusNotFound)
}

func TestViewingOrdersNeverStallsTheAPI(t *testing.T) {
	e := newEnv(t)
	a, b := e.login(alice), e.login(bob)
	e.addToCart(a, keyboard, 1)
	path := fmt.Sprintf("/api/orders/%v", e.checkout(a).body["id"])

	// Mix found and not-found lookups well past the connection pool size.
	for range 40 {
		e.do("GET", path, a, nil)
		e.do("GET", path, b, nil)
		e.do("GET", "/api/orders", a, nil)
	}
	expectStatus(t, e.do("GET", "/api/products?q=mug", "", nil), http.StatusOK)
}

func urlQuery(s string) string {
	r := strings.NewReplacer("%", "%25", " ", "%20", "'", "%27", "\"", "%22", ";", "%3B", "=", "%3D")
	return r.Replace(s)
}
