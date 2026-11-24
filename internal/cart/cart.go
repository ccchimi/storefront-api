// Package cart keeps shopping carts in memory, one per user.
package cart

import (
	"maps"
	"slices"
	"sync"
)

// Line is one product in a cart.
type Line struct {
	ProductID int64
	Quantity  int
}

// Store holds every user's cart. The zero value is not usable; call New.
type Store struct {
	mu    sync.Mutex
	carts map[int64]map[int64]int // user -> product -> quantity
}

func New() *Store { return &Store{carts: make(map[int64]map[int64]int)} }

// Add increases the quantity of a product in the user's cart.
func (s *Store) Add(userID, productID int64, qty int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c := s.carts[userID]
	if c == nil {
		// Always a fresh map. Handing out a shared "empty cart" is exactly how
		// items end up showing in somebody else's cart.
		c = make(map[int64]int)
		s.carts[userID] = c
	}
	c[productID] += qty
}

// Remove drops a product from the cart. It reports whether it was there.
func (s *Store) Remove(userID, productID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	c := s.carts[userID]
	if _, ok := c[productID]; !ok {
		return false
	}
	delete(c, productID)
	if len(c) == 0 {
		delete(s.carts, userID)
	}
	return true
}

// Lines returns a copy of the cart sorted by product ID, so callers can't
// mutate the stored cart and two snapshots of the same cart compare equal.
func (s *Store) Lines(userID int64) []Line {
	s.mu.Lock()
	defer s.mu.Unlock()

	c := s.carts[userID]
	lines := make([]Line, 0, len(c))
	for _, id := range slices.Sorted(maps.Keys(c)) {
		lines = append(lines, Line{ProductID: id, Quantity: c[id]})
	}
	return lines
}

// Clear empties the user's cart.
func (s *Store) Clear(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.carts, userID)
}
