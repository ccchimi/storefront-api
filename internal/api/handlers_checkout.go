package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ccchimi/storefront-api/internal/cart"
	"github.com/ccchimi/storefront-api/internal/email"
	"github.com/ccchimi/storefront-api/internal/money"
	"github.com/ccchimi/storefront-api/internal/payment"
	"github.com/ccchimi/storefront-api/internal/store"
)

const (
	maxDistinctProducts  = 20
	maxIdempotencyKeyLen = 255
)

// response is a fully rendered reply. Checkout produces one instead of
// writing straight to the ResponseWriter so the exact bytes can be stored
// and replayed for idempotent retries.
type response struct {
	status int
	body   []byte
}

func reply(status int, v any) response { return response{status: status, body: encode(v)} }

func (a *App) handleCheckout(w http.ResponseWriter, r *http.Request, user store.User) {
	if ok, retry := a.checkoutLimit.Allow(strconv.FormatInt(user.ID, 10)); !ok {
		writeRateLimited(w, retry)
		return
	}

	key := r.Header.Get("Idempotency-Key")
	if len(key) > maxIdempotencyKeyLen {
		writeValidation(w, map[string]string{"Idempotency-Key": "must be at most 255 characters"})
		return
	}

	lines := a.carts.Lines(user.ID)
	if key == "" {
		res := a.checkout(r.Context(), user, lines)
		writeRaw(w, res.status, res.body)
		return
	}
	a.checkoutIdempotent(w, r, user, key, lines)
}

// checkoutIdempotent runs a checkout at most once per (user, key).
//
//   - first time: reserve the key, run the checkout, store the response.
//   - key completed: replay the stored response byte for byte, even though
//     the cart is empty by now. A non-empty cart with different contents
//     means the key is being reused for another purchase: 422.
//   - key still processing: 409, the client should wait and retry.
//   - checkout failed: the key is released so the same key can be retried.
func (a *App) checkoutIdempotent(w http.ResponseWriter, r *http.Request, user store.User, key string, lines []cart.Line) {
	ctx := r.Context()
	fingerprint := cartFingerprint(lines)

	reserved, prev, err := a.store.ReserveIdempotencyKey(ctx, user.ID, key, fingerprint)
	if err != nil {
		a.log.Error("reserve idempotency key", "err", err)
		writeInternal(w)
		return
	}
	if !reserved {
		switch {
		case !prev.Completed:
			writeError(w, http.StatusConflict, "IDEMPOTENCY_KEY_IN_USE",
				"A request with this Idempotency-Key is still being processed")
		case len(lines) > 0 && prev.RequestHash != fingerprint:
			writeError(w, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED",
				"This Idempotency-Key was used for a different cart")
		default:
			w.Header().Set("Idempotent-Replayed", "true")
			writeRaw(w, prev.Status, prev.Body)
		}
		return
	}

	res := a.checkout(ctx, user, lines)

	// The outcome must be recorded even if the client hung up meanwhile;
	// otherwise the key would stay stuck in "processing".
	saveCtx := context.WithoutCancel(ctx)
	if res.status >= 200 && res.status < 300 {
		err = a.store.CompleteIdempotencyKey(saveCtx, user.ID, key, res.status, res.body)
	} else {
		err = a.store.ReleaseIdempotencyKey(saveCtx, user.ID, key)
	}
	if err != nil {
		a.log.Error("finish idempotency key", "err", err, "status", res.status)
	}
	writeRaw(w, res.status, res.body)
}

// checkout validates the cart, charges the customer and records the order.
// It is all-or-nothing: if anything fails, nothing is written, the customer
// isn't charged (or gets refunded) and the cart stays as it was.
func (a *App) checkout(ctx context.Context, user store.User, lines []cart.Line) response {
	switch {
	case len(lines) == 0:
		return reply(http.StatusBadRequest, validationError(map[string]string{"cart": "must not be empty"}))
	case len(lines) > maxDistinctProducts:
		return reply(http.StatusBadRequest, validationError(map[string]string{
			"cart": fmt.Sprintf("must contain at most %d distinct products", maxDistinctProducts),
		}))
	}

	// Hold the products' locks from the stock check until the order is
	// committed. Without this, two customers could both see the last unit,
	// both get charged, and one of them would then need a refund.
	ids := productIDs(lines)
	unlock := a.productLocks.lockAll(ids)
	defer unlock()

	products, err := a.store.ProductsByID(ctx, ids)
	if err != nil {
		a.log.Error("load products for checkout", "err", err)
		return reply(http.StatusInternalServerError, newError("INTERNAL", "Something went wrong"))
	}

	items := make([]store.OrderItem, 0, len(lines))
	var total money.Cents
	for _, l := range lines {
		p, ok := products[l.ProductID]
		if !ok || p.Stock < l.Quantity {
			return outOfStock(l.ProductID, p.Name)
		}
		lineTotal := p.Price.Mul(l.Quantity)
		total += lineTotal
		items = append(items, store.OrderItem{
			ProductID: p.ID, Name: p.Name, UnitPrice: p.Price, Quantity: l.Quantity, LineTotal: lineTotal,
		})
	}

	// Charge before writing anything. The order is only recorded once the
	// money is secured, so a declined card leaves no trace in the database.
	chargeID, err := a.payments.Charge(ctx, payment.Charge{
		UserID: user.ID, Amount: total, Reference: "chk_" + rand.Text()[:16],
	})
	if errors.Is(err, payment.ErrDeclined) {
		return reply(http.StatusPaymentRequired, newError("PAYMENT_DECLINED", "The payment was declined"))
	}
	if err != nil {
		a.log.Error("charge payment", "err", err)
		return reply(http.StatusBadGateway, newError("PAYMENT_UNAVAILABLE", "The payment provider could not be reached"))
	}

	// From here on the customer has paid, so a cancelled request must not
	// abort the write halfway.
	writeCtx := context.WithoutCancel(ctx)
	compose := func(orderID int64) store.EmailDraft { return confirmationEmail(user, orderID, items, total) }
	order, emailID, err := a.store.PlaceOrder(writeCtx, store.NewOrder{
		UserID: user.ID, Items: items, Total: total, PaymentRef: chargeID, ComposeEmail: compose,
	})
	if err != nil {
		a.refund(chargeID)
		var oos *store.OutOfStockError
		if errors.As(err, &oos) {
			// Shouldn't happen while we hold the product locks, but the database
			// constraint is the final word, so handle it properly anyway.
			return outOfStock(oos.ProductID, products[oos.ProductID].Name)
		}
		a.log.Error("place order", "err", err)
		return reply(http.StatusInternalServerError, newError("INTERNAL", "Something went wrong"))
	}

	a.carts.Clear(user.ID)
	a.products.Invalidate() // listings must show the new stock right away
	draft := compose(order.ID)
	a.sendConfirmation(emailID, email.Message{To: draft.To, Subject: draft.Subject, Body: draft.Body})

	return reply(http.StatusCreated, toOrderJSON(order))
}

func outOfStock(productID int64, name string) response {
	msg := fmt.Sprintf("Product %d does not have enough stock", productID)
	if name != "" {
		msg = fmt.Sprintf("%q does not have enough stock", name)
	}
	return reply(http.StatusConflict, newError("OUT_OF_STOCK", msg))
}

// refund gives the money back after a charge whose order couldn't be saved.
// If even that fails, the log line is what support needs to fix it by hand.
func (a *App) refund(chargeID string) {
	if err := a.payments.Refund(context.Background(), chargeID); err != nil {
		a.log.Error("refund failed, manual follow-up needed", "charge_id", chargeID, "err", err)
	}
}

// sendConfirmation delivers the email in the background. The customer gets
// the checkout response without waiting for the vendor, and Close waits for
// these goroutines so a restart doesn't drop mail.
func (a *App) sendConfirmation(emailID int64, msg email.Message) {
	a.background.Add(1)
	go func() {
		defer a.background.Done()

		// Not the request context: that one is cancelled as soon as the
		// response is written, which would kill the send.
		ctx, cancel := context.WithTimeout(context.Background(), a.cfg.EmailTimeout)
		defer cancel()

		err := a.mailer.Send(ctx, msg)
		if err != nil {
			a.log.Error("send confirmation email", "email_id", emailID, "err", err)
		}
		if err := a.store.MarkEmail(context.Background(), emailID, err == nil); err != nil {
			a.log.Error("record email status", "email_id", emailID, "err", err)
		}
	}()
}

// cartFingerprint identifies a cart's contents. Lines come sorted by product
// ID, so the same cart always produces the same fingerprint.
func cartFingerprint(lines []cart.Line) string {
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "%d:%d;", l.ProductID, l.Quantity)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func confirmationEmail(user store.User, orderID int64, items []store.OrderItem, total money.Cents) store.EmailDraft {
	var b strings.Builder
	fmt.Fprintf(&b, "Hi %s,\n\nThanks for your order #%d. Here's what you bought:\n\n", user.Name, orderID)
	for _, it := range items {
		fmt.Fprintf(&b, "  %d x %s @ $%s = $%s\n", it.Quantity, it.Name, it.UnitPrice, it.LineTotal)
	}
	fmt.Fprintf(&b, "\nTotal: $%s\n", total)
	return store.EmailDraft{
		To:      user.Email,
		Subject: fmt.Sprintf("Your order #%d is confirmed", orderID),
		Body:    b.String(),
	}
}
