package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/ccchimi/storefront-api/internal/store"
)

const maxQueryLen = 100

type productJSON struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PriceCents  int64  `json:"price_cents"`
	Price       string `json:"price"`
	Stock       int    `json:"stock"`
	InStock     bool   `json:"in_stock"`
}

func toProductJSON(p store.Product) productJSON {
	return productJSON{
		ID: p.ID, Name: p.Name, Description: p.Description,
		PriceCents: int64(p.Price), Price: p.Price.String(),
		Stock: p.Stock, InStock: p.Stock > 0,
	}
}

func (a *App) handleListProducts(w http.ResponseWriter, r *http.Request) {
	if ok, retry := a.productsLimit.Allow(clientIP(r)); !ok {
		writeRateLimited(w, retry)
		return
	}

	query := r.URL.Query().Get("q")
	if utf8.RuneCountInString(query) > maxQueryLen {
		writeValidation(w, map[string]string{"q": "must be at most 100 characters"})
		return
	}

	// LIKE is case-insensitive for ASCII, so "Mug" and "mug" share a cache entry.
	cacheKey := strings.ToLower(query)
	products, gen, hit := a.products.Get(cacheKey)
	if !hit {
		var err error
		products, err = a.store.SearchProducts(r.Context(), query)
		if err != nil {
			a.log.Error("search products", "err", err)
			writeInternal(w)
			return
		}
		a.products.Put(cacheKey, gen, products)
	}

	items := make([]productJSON, len(products))
	for i, p := range products {
		items[i] = toProductJSON(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
