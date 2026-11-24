package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ccchimi/storefront-api/internal/money"
)

// DemoPassword is the password of every seeded user.
const DemoPassword = "password123"

// DemoUsers are created on first start so the API is usable right away.
var DemoUsers = []struct{ Email, Name string }{
	{"alice@storefront.test", "Alice"},
	{"bob@storefront.test", "Bob"},
	{"carol@storefront.test", "Carol"},
	{"dave@storefront.test", "Dave"},
	{"erin@storefront.test", "Erin"},
}

var demoProducts = []Product{
	{Name: "Mechanical Keyboard", Description: "Tenkeyless, hot-swappable switches", Price: 8999, Stock: 25},
	{Name: "Wireless Mouse", Description: "Silent clicks, USB-C charging", Price: 2499, Stock: 50},
	{Name: "USB-C Hub", Description: "7-in-1 with HDMI and SD card reader", Price: 3999, Stock: 30},
	{Name: "Dotted Notebook", Description: "A5, 160 pages", Price: 749, Stock: 200},
	{Name: "Coffee Mug", Description: "Ceramic, 350 ml", Price: 1250, Stock: 80},
	{Name: "Desk Lamp", Description: "Dimmable LED with warm light mode", Price: 3400, Stock: 15},
	{Name: "Laptop Stand", Description: "Aluminium, adjustable height", Price: 4500, Stock: 20},
	{Name: "Noise-Cancelling Headphones", Description: "Over-ear, 30h battery", Price: 19999, Stock: 10},
	{Name: "Webcam", Description: "1080p with built-in microphone", Price: 5999, Stock: 18},
	{Name: "Water Bottle", Description: "Insulated steel, 750 ml", Price: 1999, Stock: 100},
	{Name: "Sticker Pack", Description: "20 developer-themed stickers", Price: 499, Stock: 500},
	{Name: "Monitor Arm", Description: "Single arm, gas spring", Price: 7990, Stock: 8},
	{Name: "Cable Organizer", Description: "Silicone clips, set of 6", Price: 995, Stock: 120},
	{Name: "Ergonomic Chair", Description: "Mesh back, lumbar support", Price: 34900, Stock: 5},
	{Name: "Desk Mat", Description: "Felt, 90 x 40 cm", Price: 2200, Stock: 40},
	{Name: "Portable SSD", Description: "1 TB, USB 3.2", Price: 10999, Stock: 12},
	{Name: "Phone Stand", Description: "Foldable, fits most phones", Price: 1499, Stock: 60},
	{Name: "Bluetooth Speaker", Description: "Water resistant, 12h battery", Price: 4999, Stock: 22},
	{Name: "HDMI Cable", Description: "2 m, 4K at 60 Hz", Price: 1199, Stock: 150},
	{Name: "Wrist Rest", Description: "Memory foam, keyboard size", Price: 1650, Stock: 35},
	{Name: "Fountain Pen", Description: "Medium nib, refillable", Price: 2775, Stock: 25},
	{Name: "Limited Edition Hoodie", Description: "Only one left, ever", Price: 6400, Stock: 1},
	{Name: "Plant Pot", Description: "Concrete, with drainage", Price: 1330, Stock: 45},
	{Name: "Smart Plug", Description: "Wi-Fi, energy monitoring", Price: 1825, Stock: 70},
}

// Seed adds the demo users and products if they aren't there yet. It never
// touches existing data, so restarting the server keeps orders around.
// hash turns the demo password into a stored hash; it's only called for
// users that actually need creating, because password hashing is slow.
func (s *Store) Seed(ctx context.Context, hash func(string) (string, error)) error {
	for _, u := range DemoUsers {
		_, err := s.UserByEmail(ctx, u.Email)
		if err == nil {
			continue
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		h, err := hash(DemoPassword)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO users (email, name, password_hash, created_at) VALUES (?, ?, ?, ?)`,
			u.Email, u.Name, h, formatTime(s.now())); err != nil {
			return err
		}
	}

	return s.inTx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM products`).Scan(&count); err != nil || count > 0 {
			return err
		}
		for _, p := range demoProducts {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO products (name, description, price_cents, stock) VALUES (?, ?, ?, ?)`,
				p.Name, p.Description, p.Price, p.Stock); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetStock overwrites a product's stock. Handy for demos and tests.
func (s *Store) SetStock(ctx context.Context, productID int64, stock int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE products SET stock = ? WHERE id = ?`, stock, productID)
	return err
}

// SetPrice overwrites a product's price.
func (s *Store) SetPrice(ctx context.Context, productID int64, price money.Cents) error {
	_, err := s.db.ExecContext(ctx, `UPDATE products SET price_cents = ? WHERE id = ?`, price, productID)
	return err
}
