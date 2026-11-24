package api

import (
	"slices"
	"sync"
)

// keyedMutex hands out one lock per product ID. Checkouts that share a
// product run one after another; checkouts for unrelated products don't wait
// for each other.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[int64]*refMutex
}

type refMutex struct {
	sync.Mutex
	refs int
}

// lockAll locks every ID and returns a function that unlocks them. IDs are
// always taken in ascending order, so two checkouts can never hold one lock
// each while waiting for the other's (no deadlock).
func (k *keyedMutex) lockAll(ids []int64) (unlock func()) {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)

	held := make([]*refMutex, 0, len(ids))
	for _, id := range ids {
		m := k.acquire(id)
		m.Lock()
		held = append(held, m)
	}
	return func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].Unlock()
			k.release(ids[i])
		}
	}
}

func (k *keyedMutex) acquire(id int64) *refMutex {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locks == nil {
		k.locks = make(map[int64]*refMutex)
	}
	m := k.locks[id]
	if m == nil {
		m = &refMutex{}
		k.locks[id] = m
	}
	m.refs++
	return m
}

// release drops the entry once nobody uses it, so the map doesn't grow with
// every product ever bought.
func (k *keyedMutex) release(id int64) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if m := k.locks[id]; m != nil {
		m.refs--
		if m.refs == 0 {
			delete(k.locks, id)
		}
	}
}
