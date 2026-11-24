package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ccchimi/storefront-api/internal/store"
)

const (
	defaultPageSize = 10
	maxPageSize     = 50
)

type orderJSON struct {
	ID         int64           `json:"id"`
	Status     string          `json:"status"`
	TotalCents int64           `json:"total_cents"`
	Total      string          `json:"total"`
	ItemCount  int             `json:"item_count"`
	CreatedAt  string          `json:"created_at"`
	Items      []orderItemJSON `json:"items,omitempty"`
	Invoice    *invoiceJSON    `json:"invoice,omitempty"`
}

type orderItemJSON struct {
	ProductID      int64  `json:"product_id"`
	Name           string `json:"name"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	Quantity       int    `json:"quantity"`
	LineTotalCents int64  `json:"line_total_cents"`
}

type invoiceJSON struct {
	Number      string `json:"number"`
	AmountCents int64  `json:"amount_cents"`
	Amount      string `json:"amount"`
	IssuedAt    string `json:"issued_at"`
}

func toOrderJSON(o store.Order) orderJSON {
	out := orderJSON{
		ID: o.ID, Status: o.Status, TotalCents: int64(o.Total), Total: o.Total.String(),
		ItemCount: o.ItemCount, CreatedAt: o.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if o.Items != nil {
		out.Items = make([]orderItemJSON, len(o.Items))
		for i, it := range o.Items {
			out.Items[i] = orderItemJSON{
				ProductID: it.ProductID, Name: it.Name, UnitPriceCents: int64(it.UnitPrice),
				Quantity: it.Quantity, LineTotalCents: int64(it.LineTotal),
			}
		}
	}
	if o.Invoice != nil {
		out.Invoice = &invoiceJSON{
			Number: o.Invoice.Number, AmountCents: int64(o.Invoice.Amount), Amount: o.Invoice.Amount.String(),
			IssuedAt: o.Invoice.IssuedAt.UTC().Format(time.RFC3339Nano),
		}
	}
	return out
}

func (a *App) handleListOrders(w http.ResponseWriter, r *http.Request, user store.User) {
	fields := map[string]string{}
	page, present, ok := positiveQueryInt(r, "page")
	if !ok {
		fields["page"] = "must be a positive integer"
	} else if !present {
		page = 1
	}
	limit, present, ok := positiveQueryInt(r, "limit")
	switch {
	case !ok || limit > maxPageSize:
		fields["limit"] = "must be an integer between 1 and 50"
	case !present:
		limit = defaultPageSize
	}
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}

	// Pages are 1-based: page 1 starts at offset 0.
	orders, total, err := a.store.ListOrders(r.Context(), user.ID, limit, (page-1)*limit)
	if err != nil {
		a.log.Error("list orders", "err", err)
		writeInternal(w)
		return
	}
	items := make([]orderJSON, len(orders))
	for i, o := range orders {
		items[i] = toOrderJSON(o)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page, "limit": limit, "total": total})
}

func (a *App) handleGetOrder(w http.ResponseWriter, r *http.Request, user store.User) {
	orderID, err := strconv.ParseInt(r.PathValue("order_id"), 10, 64)
	if err != nil || orderID < 1 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Order not found")
		return
	}
	order, err := a.store.GetOrder(r.Context(), user.ID, orderID)
	if errors.Is(err, store.ErrNotFound) {
		// Someone else's order gets the same 404 as one that doesn't exist.
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Order not found")
		return
	}
	if err != nil {
		a.log.Error("get order", "err", err)
		writeInternal(w)
		return
	}
	writeJSON(w, http.StatusOK, toOrderJSON(order))
}
