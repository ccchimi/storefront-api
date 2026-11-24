package store

import (
	"context"
	"strings"

	"github.com/ccchimi/storefront-api/internal/money"
)

// Product is a catalog entry.
type Product struct {
	ID          int64
	Name        string
	Description string
	Price       money.Cents
	Stock       int
}

// SearchProducts lists products whose name or description contains query.
// An empty query lists everything.
func (s *Store) SearchProducts(ctx context.Context, query string) ([]Product, error) {
	// The search term is always a bound parameter, never part of the SQL text.
	// Wildcards typed by the user are escaped so "50%" means a literal 50%.
	pattern := "%" + escapeLike(query) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, price_cents, stock
		FROM products
		WHERE name LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\'
		ORDER BY id`, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	products := []Product{}
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock); err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

// ProductsByID loads the given products, keyed by ID. Missing IDs are
// simply absent from the result.
func (s *Store) ProductsByID(ctx context.Context, ids []int64) (map[int64]Product, error) {
	out := make(map[int64]Product, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, price_cents, stock FROM products WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock); err != nil {
			return nil, err
		}
		out[p.ID] = p
	}
	return out, rows.Err()
}

// ProductExists reports whether a product ID is in the catalog.
func (s *Store) ProductExists(ctx context.Context, id int64) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM products WHERE id = ?)`, id).Scan(&exists)
	return exists, err
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
