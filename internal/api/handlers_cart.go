package api

import (
	"net/http"
	"strconv"

	"github.com/ccchimi/storefront-api/internal/cart"
	"github.com/ccchimi/storefront-api/internal/money"
	"github.com/ccchimi/storefront-api/internal/store"
)

const (
	minQuantity = 1
	maxQuantity = 10
)

type cartLineJSON struct {
	ProductID      int64  `json:"product_id"`
	Name           string `json:"name"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	Quantity       int    `json:"quantity"`
	LineTotalCents int64  `json:"line_total_cents"`
	InStock        bool   `json:"in_stock"`
}

func (a *App) handleGetCart(w http.ResponseWriter, r *http.Request, user store.User) {
	a.writeCart(w, r, user.ID)
}

func (a *App) handleAddToCart(w http.ResponseWriter, r *http.Request, user store.User) {
	body, ok := decodeObject(r)
	if !ok {
		writeValidation(w, invalidBody())
		return
	}

	fields := map[string]string{}
	productID, ok := intField(body, "product_id")
	if !ok || productID < 1 {
		fields["product_id"] = "must be a positive integer"
	}
	// Convert to int only inside the range check, so the narrowing from
	// int64 can never overflow.
	var qty int
	if n, ok := intField(body, "quantity"); ok && n >= minQuantity && n <= maxQuantity {
		qty = int(n)
	} else {
		fields["quantity"] = "must be an integer between 1 and 10"
	}
	if _, bad := fields["product_id"]; !bad {
		exists, err := a.store.ProductExists(r.Context(), productID)
		if err != nil {
			a.log.Error("check product", "err", err)
			writeInternal(w)
			return
		}
		if !exists {
			fields["product_id"] = "must refer to an existing product"
		}
	}
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}

	a.carts.Add(user.ID, productID, qty)
	a.writeCart(w, r, user.ID)
}

func (a *App) handleRemoveFromCart(w http.ResponseWriter, r *http.Request, user store.User) {
	productID, err := strconv.ParseInt(r.PathValue("product_id"), 10, 64)
	if err != nil || productID < 1 {
		writeValidation(w, map[string]string{"product_id": "must be a positive integer"})
		return
	}
	if !a.carts.Remove(user.ID, productID) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "That product is not in your cart")
		return
	}
	a.writeCart(w, r, user.ID)
}

// writeCart renders the cart with current catalog prices.
func (a *App) writeCart(w http.ResponseWriter, r *http.Request, userID int64) {
	lines := a.carts.Lines(userID)
	products, err := a.store.ProductsByID(r.Context(), productIDs(lines))
	if err != nil {
		a.log.Error("load cart products", "err", err)
		writeInternal(w)
		return
	}

	items := make([]cartLineJSON, 0, len(lines))
	var total money.Cents
	for _, l := range lines {
		p := products[l.ProductID]
		lineTotal := p.Price.Mul(l.Quantity)
		total += lineTotal
		items = append(items, cartLineJSON{
			ProductID:      l.ProductID,
			Name:           p.Name,
			UnitPriceCents: int64(p.Price),
			Quantity:       l.Quantity,
			LineTotalCents: int64(lineTotal),
			InStock:        p.Stock >= l.Quantity,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":       items,
		"total_cents": int64(total),
		"total":       total.String(),
	})
}

func productIDs(lines []cart.Line) []int64 {
	ids := make([]int64, len(lines))
	for i, l := range lines {
		ids[i] = l.ProductID
	}
	return ids
}
