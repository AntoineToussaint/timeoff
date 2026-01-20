// Package store provides Store implementations.
package store

import (
	"context"
	"sort"
	"sync"

	"github.com/warp/resource-engine/generic"
)

// =============================================================================
// MEMORY STORE - In-memory implementation (for testing/dev)
// =============================================================================

type Memory struct {
	mu           sync.RWMutex
	transactions map[key][]generic.Transaction
	idempotency  map[string]bool
}

type key struct {
	EntityID generic.EntityID
	PolicyID generic.PolicyID
}

func NewMemory() *Memory {
	return &Memory{
		transactions: make(map[key][]generic.Transaction),
		idempotency:  make(map[string]bool),
	}
}

// Append adds a single transaction. Append-only.
func (m *Memory) Append(_ context.Context, tx generic.Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.appendLocked(tx)
}

// AppendBatch adds multiple transactions atomically.
func (m *Memory) AppendBatch(_ context.Context, txs []generic.Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check all idempotency keys first (atomic check)
	for _, tx := range txs {
		if tx.IdempotencyKey != "" && m.idempotency[tx.IdempotencyKey] {
			return generic.ErrDuplicateIdempotencyKey
		}
	}

	// Append all (atomic write)
	for _, tx := range txs {
		if err := m.appendLocked(tx); err != nil {
			return err
		}
	}
	return nil
}

func (m *Memory) appendLocked(tx generic.Transaction) error {
	k := key{EntityID: tx.EntityID, PolicyID: tx.PolicyID}
	txs := m.transactions[k]

	// Binary search for insertion point: O(log n) instead of O(n log n)
	i := sort.Search(len(txs), func(i int) bool {
		return txs[i].EffectiveAt.After(tx.EffectiveAt)
	})

	// Insert at position i: O(n) for copy, but no comparison overhead
	txs = append(txs, generic.Transaction{})
	copy(txs[i+1:], txs[i:])
	txs[i] = tx
	m.transactions[k] = txs

	if tx.IdempotencyKey != "" {
		m.idempotency[tx.IdempotencyKey] = true
	}
	return nil
}

func (m *Memory) Load(_ context.Context, entityID generic.EntityID, policyID generic.PolicyID) ([]generic.Transaction, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	k := key{EntityID: entityID, PolicyID: policyID}
	result := make([]generic.Transaction, len(m.transactions[k]))
	copy(result, m.transactions[k])
	return result, nil
}

func (m *Memory) LoadRange(_ context.Context, entityID generic.EntityID, policyID generic.PolicyID, from, to generic.TimePoint) ([]generic.Transaction, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	k := key{EntityID: entityID, PolicyID: policyID}
	var result []generic.Transaction
	for _, tx := range m.transactions[k] {
		if from.BeforeOrEqual(tx.EffectiveAt) && tx.EffectiveAt.BeforeOrEqual(to) {
			result = append(result, tx)
		}
	}
	return result, nil
}

func (m *Memory) Exists(_ context.Context, idempotencyKey string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.idempotency[idempotencyKey], nil
}

// =============================================================================
// TRANSACTIONAL MEMORY STORE
// =============================================================================

// TxMemory wraps Memory with transaction support.
type TxMemory struct {
	*Memory
}

func NewTxMemory() *TxMemory {
	return &TxMemory{Memory: NewMemory()}
}

// WithTx executes fn within a transaction.
// For memory store, this is simulated with a snapshot + rollback on error.
func (tm *TxMemory) WithTx(ctx context.Context, fn func(generic.Store) error) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Snapshot current state
	snapshot := tm.snapshot()

	// Create a transactional view
	txStore := &txMemoryView{parent: tm}

	// Execute function
	if err := fn(txStore); err != nil {
		// Rollback
		tm.restore(snapshot)
		return err
	}

	// Commit (already done via direct writes)
	return nil
}

func (tm *TxMemory) snapshot() memorySnapshot {
	txsCopy := make(map[key][]generic.Transaction)
	for k, v := range tm.transactions {
		txsCopy[k] = append([]generic.Transaction{}, v...)
	}
	idempCopy := make(map[string]bool)
	for k, v := range tm.idempotency {
		idempCopy[k] = v
	}
	return memorySnapshot{transactions: txsCopy, idempotency: idempCopy}
}

func (tm *TxMemory) restore(s memorySnapshot) {
	tm.transactions = s.transactions
	tm.idempotency = s.idempotency
}

type memorySnapshot struct {
	transactions map[key][]generic.Transaction
	idempotency  map[string]bool
}

type txMemoryView struct {
	parent *TxMemory
}

func (tv *txMemoryView) Append(ctx context.Context, tx generic.Transaction) error {
	return tv.parent.appendLocked(tx)
}

func (tv *txMemoryView) AppendBatch(ctx context.Context, txs []generic.Transaction) error {
	for _, tx := range txs {
		if err := tv.parent.appendLocked(tx); err != nil {
			return err
		}
	}
	return nil
}

func (tv *txMemoryView) Load(ctx context.Context, entityID generic.EntityID, policyID generic.PolicyID) ([]generic.Transaction, error) {
	k := key{EntityID: entityID, PolicyID: policyID}
	return tv.parent.transactions[k], nil
}

func (tv *txMemoryView) LoadRange(ctx context.Context, entityID generic.EntityID, policyID generic.PolicyID, from, to generic.TimePoint) ([]generic.Transaction, error) {
	k := key{EntityID: entityID, PolicyID: policyID}
	var result []generic.Transaction
	for _, tx := range tv.parent.transactions[k] {
		if from.BeforeOrEqual(tx.EffectiveAt) && tx.EffectiveAt.BeforeOrEqual(to) {
			result = append(result, tx)
		}
	}
	return result, nil
}

func (tv *txMemoryView) Exists(_ context.Context, idempotencyKey string) (bool, error) {
	return tv.parent.idempotency[idempotencyKey], nil
}
