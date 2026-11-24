package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ccchimi/storefront-api/internal/money"
)

// Order is a placed order. Items and Invoice are only filled by GetOrder.
type Order struct {
	ID         int64
	UserID     int64
	Status     string
	Total      money.Cents
	PaymentRef string
	CreatedAt  time.Time
	ItemCount  int
	Items      []OrderItem
	Invoice    *Invoice
}

// OrderItem is one line of an order, priced at the time of purchase.
type OrderItem struct {
	ProductID int64
	Name      string
	UnitPrice money.Cents
	Quantity  int
	LineTotal money.Cents
}

// Invoice is the billing document issued for an order.
type Invoice struct {
	Number   string
	Amount   money.Cents
	IssuedAt time.Time
}

// OutOfStockError reports the product that couldn't be fulfilled.
type OutOfStockError struct{ ProductID int64 }

func (e *OutOfStockError) Error() string {
	return fmt.Sprintf("product %d does not have enough stock", e.ProductID)
}

// NewOrder is everything needed to record a paid order.
type NewOrder struct {
	UserID     int64
	Items      []OrderItem
	Total      money.Cents
	PaymentRef string
	// ComposeEmail builds the confirmation email once the order ID is known.
	// The email is recorded in the same transaction, so an order can never
	// exist without its email (or the other way round).
	ComposeEmail func(orderID int64) EmailDraft
}

// EmailDraft is a confirmation email waiting to be sent.
type EmailDraft struct {
	To      string
	Subject string
	Body    string
}

// PlaceOrder records an order, its items, the stock decrement, the invoice
// and the confirmation email in a single transaction: either all of it is
// written or none of it is.
func (s *Store) PlaceOrder(ctx context.Context, o NewOrder) (order Order, emailID int64, err error) {
	now := s.now()
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO orders (user_id, status, total_cents, payment_ref, created_at) VALUES (?, 'paid', ?, ?, ?)`,
			o.UserID, o.Total, o.PaymentRef, formatTime(now))
		if err != nil {
			return err
		}
		orderID, err := res.LastInsertId()
		if err != nil {
			return err
		}

		for _, it := range o.Items {
			// The stock check and the decrement are one statement, so two
			// buyers can't both see "1 left" and both take it.
			res, err := tx.ExecContext(ctx,
				`UPDATE products SET stock = stock - ? WHERE id = ? AND stock >= ?`,
				it.Quantity, it.ProductID, it.Quantity)
			if err != nil {
				return err
			}
			if n, err := res.RowsAffected(); err != nil {
				return err
			} else if n == 0 {
				return &OutOfStockError{ProductID: it.ProductID}
			}

			if _, err := tx.ExecContext(ctx, `
				INSERT INTO order_items (order_id, product_id, product_name, unit_price_cents, quantity, line_total_cents)
				VALUES (?, ?, ?, ?, ?, ?)`,
				orderID, it.ProductID, it.Name, it.UnitPrice, it.Quantity, it.LineTotal); err != nil {
				return err
			}
		}

		invoice := Invoice{Number: fmt.Sprintf("INV-%08d", orderID), Amount: o.Total, IssuedAt: now}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO invoices (order_id, number, amount_cents, issued_at) VALUES (?, ?, ?, ?)`,
			orderID, invoice.Number, invoice.Amount, formatTime(now)); err != nil {
			return err
		}

		draft := o.ComposeEmail(orderID)
		res, err = tx.ExecContext(ctx, `
			INSERT INTO emails (order_id, recipient, subject, body, status, created_at)
			VALUES (?, ?, ?, ?, 'queued', ?)`,
			orderID, draft.To, draft.Subject, draft.Body, formatTime(now))
		if err != nil {
			return err
		}
		if emailID, err = res.LastInsertId(); err != nil {
			return err
		}

		order = Order{
			ID: orderID, UserID: o.UserID, Status: "paid", Total: o.Total, PaymentRef: o.PaymentRef,
			CreatedAt: now, ItemCount: len(o.Items), Items: o.Items, Invoice: &invoice,
		}
		return nil
	})
	return order, emailID, err
}

// ListOrders returns one page of a user's orders, newest first, plus the
// user's total order count.
func (s *Store) ListOrders(ctx context.Context, userID int64, limit, offset int) ([]Order, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders WHERE user_id = ?`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id, o.user_id, o.status, o.total_cents, o.payment_ref, o.created_at,
		       (SELECT COUNT(*) FROM order_items i WHERE i.order_id = o.id)
		FROM orders o
		WHERE o.user_id = ?
		ORDER BY o.created_at DESC, o.id DESC
		LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	orders := []Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, 0, err
		}
		orders = append(orders, o)
	}
	return orders, total, rows.Err()
}

// GetOrder loads an order with its items and invoice. An order that belongs
// to someone else is reported as ErrNotFound, so its existence isn't leaked.
func (s *Store) GetOrder(ctx context.Context, userID, orderID int64) (Order, error) {
	o, err := scanOrder(s.db.QueryRowContext(ctx, `
		SELECT o.id, o.user_id, o.status, o.total_cents, o.payment_ref, o.created_at,
		       (SELECT COUNT(*) FROM order_items i WHERE i.order_id = o.id)
		FROM orders o
		WHERE o.id = ? AND o.user_id = ?`, orderID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}

	// The order row is fully read before the next query runs; holding a
	// cursor open while asking for another connection is a classic way to
	// starve a small pool.
	rows, err := s.db.QueryContext(ctx, `
		SELECT product_id, product_name, unit_price_cents, quantity, line_total_cents
		FROM order_items WHERE order_id = ? ORDER BY id`, orderID)
	if err != nil {
		return Order{}, err
	}
	defer rows.Close()

	o.Items = []OrderItem{}
	for rows.Next() {
		var it OrderItem
		if err := rows.Scan(&it.ProductID, &it.Name, &it.UnitPrice, &it.Quantity, &it.LineTotal); err != nil {
			return Order{}, err
		}
		o.Items = append(o.Items, it)
	}
	if err := rows.Err(); err != nil {
		return Order{}, err
	}

	var inv Invoice
	var issued string
	err = s.db.QueryRowContext(ctx,
		`SELECT number, amount_cents, issued_at FROM invoices WHERE order_id = ?`, orderID).
		Scan(&inv.Number, &inv.Amount, &issued)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return Order{}, err
	default:
		if inv.IssuedAt, err = parseTime(issued); err != nil {
			return Order{}, err
		}
		o.Invoice = &inv
	}
	return o, nil
}

// MarkEmail records the delivery outcome of a confirmation email.
func (s *Store) MarkEmail(ctx context.Context, id int64, sent bool) error {
	status, sentAt := "failed", sql.NullString{}
	if sent {
		status, sentAt = "sent", sql.NullString{String: formatTime(s.now()), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE emails SET status = ?, sent_at = ? WHERE id = ?`, status, sentAt, id)
	return err
}

type scanner interface{ Scan(dest ...any) error }

func scanOrder(row scanner) (Order, error) {
	var o Order
	var created string
	if err := row.Scan(&o.ID, &o.UserID, &o.Status, &o.Total, &o.PaymentRef, &created, &o.ItemCount); err != nil {
		return Order{}, err
	}
	t, err := parseTime(created)
	if err != nil {
		return Order{}, err
	}
	o.CreatedAt = t
	return o, nil
}
