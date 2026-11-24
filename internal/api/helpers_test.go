package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/ccchimi/storefront-api/internal/api"
	"github.com/ccchimi/storefront-api/internal/email"
	"github.com/ccchimi/storefront-api/internal/payment"
	"github.com/ccchimi/storefront-api/internal/store"
)

// Seeded product IDs used across the tests.
const (
	keyboard    = 1  // $89.99, 25 in stock
	waterBottle = 10 // $19.99, 100 in stock
	chair       = 14 // $349.00, 5 in stock
	hoodie      = 22 // $64.00, 1 in stock
)

const (
	alice = "alice@storefront.test"
	bob   = "bob@storefront.test"
	carol = "carol@storefront.test"
	dave  = "dave@storefront.test"
	erin  = "erin@storefront.test"
)

// fakeGateway records charges and can be told to decline or to be slow.
type fakeGateway struct {
	mu      sync.Mutex
	charges []payment.Charge
	refunds []string

	decline atomic.Bool
	latency atomic.Int64 // nanoseconds
}

func (g *fakeGateway) Charge(ctx context.Context, c payment.Charge) (string, error) {
	if d := time.Duration(g.latency.Load()); d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if g.decline.Load() {
		return "", payment.ErrDeclined
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.charges = append(g.charges, c)
	return fmt.Sprintf("ch_test_%d", len(g.charges)), nil
}

func (g *fakeGateway) Refund(_ context.Context, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refunds = append(g.refunds, id)
	return nil
}

func (g *fakeGateway) chargeCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.charges)
}

// fakeMailer records delivered messages after an optional delay.
type fakeMailer struct {
	mu      sync.Mutex
	sent    []email.Message
	latency time.Duration
}

func (m *fakeMailer) Send(ctx context.Context, msg email.Message) error {
	if m.latency > 0 {
		select {
		case <-time.After(m.latency):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *fakeMailer) delivered() []email.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]email.Message(nil), m.sent...)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// env is a running API backed by a fresh database in a temp dir.
type env struct {
	t      *testing.T
	dbPath string
	app    *api.App
	srv    *httptest.Server
	db     *sql.DB // separate handle for assertions
	pay    *fakeGateway
	mail   *fakeMailer
	clock  *clock
	closed bool
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		t:      t,
		dbPath: filepath.Join(t.TempDir(), "test.db"),
		pay:    &fakeGateway{},
		mail:   &fakeMailer{},
		clock:  &clock{t: time.Now().UTC()},
	}
	e.start()

	db, err := sql.Open("sqlite3", e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	e.db = db
	t.Cleanup(func() {
		e.stop()
		db.Close()
	})
	return e
}

func (e *env) start() {
	e.t.Helper()
	app, err := api.New(context.Background(), api.Config{
		DBPath:             e.dbPath,
		Payments:           e.pay,
		Email:              e.mail,
		Now:                e.clock.Now,
		PasswordIterations: 1000, // keep tests fast; production uses the default
	})
	if err != nil {
		e.t.Fatal(err)
	}
	e.app = app
	e.srv = httptest.NewServer(app.Handler())
	e.closed = false
}

// stop shuts the server down and closes the App, which waits for emails.
func (e *env) stop() {
	if e.closed {
		return
	}
	e.closed = true
	e.srv.Close()
	if err := e.app.Close(); err != nil {
		e.t.Errorf("close app: %v", err)
	}
}

// restart simulates a process restart against the same database file.
func (e *env) restart() {
	e.t.Helper()
	e.stop()
	e.start()
}

type result struct {
	status int
	header http.Header
	raw    []byte
	body   map[string]any
}

func (r result) errorCode() string {
	errObj, _ := r.body["error"].(map[string]any)
	code, _ := errObj["code"].(string)
	return code
}

func (r result) fields() map[string]any {
	errObj, _ := r.body["error"].(map[string]any)
	f, _ := errObj["fields"].(map[string]any)
	return f
}

// do sends a request. body may be nil, a string (sent verbatim) or any value
// to be JSON-encoded. Extra headers come in name/value pairs.
func (e *env) do(method, path, token string, body any, headers ...string) result {
	e.t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		reader = bytes.NewBufferString(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, reader)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	res := result{status: resp.StatusCode, header: resp.Header, raw: raw}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &res.body)
	}
	return res
}

func (e *env) login(emailAddr string) string {
	e.t.Helper()
	res := e.do("POST", "/api/auth/login", "", map[string]any{"email": emailAddr, "password": store.DemoPassword})
	if res.status != http.StatusOK {
		e.t.Fatalf("login %s: status %d, body %s", emailAddr, res.status, res.raw)
	}
	return res.body["token"].(string)
}

func (e *env) addToCart(token string, productID, qty int) {
	e.t.Helper()
	res := e.do("POST", "/api/cart/items", token, map[string]any{"product_id": productID, "quantity": qty})
	if res.status != http.StatusOK {
		e.t.Fatalf("add to cart: status %d, body %s", res.status, res.raw)
	}
}

func (e *env) checkout(token string, headers ...string) result {
	e.t.Helper()
	return e.do("POST", "/api/checkout", token, nil, headers...)
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(query, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (e *env) stock(productID int) int {
	return e.count(`SELECT stock FROM products WHERE id = ?`, productID)
}

func (e *env) setStock(productID, stock int) {
	e.t.Helper()
	if _, err := e.db.Exec(`UPDATE products SET stock = ? WHERE id = ?`, stock, productID); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) cartSize(token string) int {
	e.t.Helper()
	res := e.do("GET", "/api/cart", token, nil)
	return len(res.body["items"].([]any))
}

func expectStatus(t *testing.T, res result, want int) {
	t.Helper()
	if res.status != want {
		t.Fatalf("status = %d, want %d (body: %s)", res.status, want, res.raw)
	}
}
