/*
Package sqlite provides a SQLite-backed implementation of the storage interfaces.

PURPOSE:
  Implements all persistence interfaces (Store, AssignmentStore, SnapshotStore)
  using SQLite. In production, the same patterns apply to PostgreSQL - only
  minor SQL dialect differences.

INTERFACES IMPLEMENTED:
  generic.Store:           Transaction persistence
  generic.AssignmentStore: Policy-to-entity mappings
  generic.SnapshotStore:   Balance snapshots

APPEND-ONLY ENFORCEMENT:
  The Store enforces append-only semantics:
  - No UPDATE statements on transactions table
  - No DELETE statements on transactions table
  - Corrections via reversal transactions only

KEY TABLES:
  transactions:       Immutable ledger of all balance changes
  policies:           Policy definitions (versioned)
  policy_assignments: Entity-to-policy links
  employees:          Entity records
  balance_snapshots:  Cached balance calculations

INDEXES:
  Critical indexes for performance:
  - idx_transactions_entity_policy_date: Balance calculation (hot path)
  - idx_transactions_entity_resource_date: Day uniqueness checks
  - idx_unique_day_consumption: Enforces no duplicate day-off
  - idx_transactions_reference: Request tracking

CONCURRENCY:
  Uses sync.RWMutex for thread-safety. In production with PostgreSQL,
  database-level concurrency control handles this instead.

WAL MODE:
  SQLite is opened with WAL (Write-Ahead Logging) for better concurrency:
  - Multiple readers don't block
  - Single writer at a time
  - Better crash recovery

USAGE:
  store, err := sqlite.New("./data/warp.db")
  if err != nil {
      log.Fatal(err)
  }
  defer store.Close()

  // Use with ledger
  ledger := generic.NewLedger(store)

MIGRATION:
  Schema is auto-migrated on New(). For production, use a proper
  migration tool (golang-migrate, goose) with versioned migrations.

SEE ALSO:
  - generic/store.go: Interface definitions
  - generic/ledger.go: Higher-level ledger using Store
  - generic/store/memory.go: In-memory implementation for testing
*/
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/warp/resource-engine/generic"
)

// Store implements all storage interfaces using SQLite.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// New creates a new SQLite store with the given database path.
// Use ":memory:" for an in-memory database.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return store, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates the database schema.
func (s *Store) migrate() error {
	schema := `
	-- Transactions (append-only ledger)
	CREATE TABLE IF NOT EXISTS transactions (
		id TEXT PRIMARY KEY,
		entity_id TEXT NOT NULL,
		policy_id TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		effective_at TEXT NOT NULL,
		delta_value TEXT NOT NULL,
		delta_unit TEXT NOT NULL,
		tx_type TEXT NOT NULL,
		reference_id TEXT,
		reason TEXT,
		idempotency_key TEXT UNIQUE,
		metadata_json TEXT,
		created_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_transactions_entity_policy 
		ON transactions(entity_id, policy_id);
	CREATE INDEX IF NOT EXISTS idx_transactions_effective_at 
		ON transactions(effective_at);
	CREATE INDEX IF NOT EXISTS idx_transactions_idempotency 
		ON transactions(idempotency_key) WHERE idempotency_key IS NOT NULL;

	-- CRITICAL: Enforce day uniqueness for time-off consumption
	-- An entity cannot have two consumption/pending transactions on the same day
	-- for the same resource type (e.g., can't take PTO twice on March 10)
	CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_day_consumption 
		ON transactions(entity_id, resource_type, DATE(effective_at))
		WHERE tx_type IN ('consumption', 'pending');

	-- For entity-wide queries (day uniqueness validation)
	CREATE INDEX IF NOT EXISTS idx_transactions_entity_resource_date 
		ON transactions(entity_id, resource_type, effective_at);

	-- For request tracking
	CREATE INDEX IF NOT EXISTS idx_transactions_reference 
		ON transactions(reference_id) WHERE reference_id IS NOT NULL;
	
	-- Composite index for period-based balance queries (hot path)
	CREATE INDEX IF NOT EXISTS idx_transactions_entity_policy_date
		ON transactions(entity_id, policy_id, effective_at DESC);
	
	-- For transaction type filtering
	CREATE INDEX IF NOT EXISTS idx_transactions_type
		ON transactions(tx_type);

	-- Policies
	CREATE TABLE IF NOT EXISTS policies (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		config_json TEXT NOT NULL,
		version INTEGER DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	-- Policy Assignments
	CREATE TABLE IF NOT EXISTS policy_assignments (
		id TEXT PRIMARY KEY,
		entity_id TEXT NOT NULL,
		policy_id TEXT NOT NULL,
		effective_from TEXT NOT NULL,
		effective_to TEXT,
		consumption_priority INTEGER DEFAULT 1,
		approval_config_json TEXT,
		created_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_assignments_entity 
		ON policy_assignments(entity_id);
	CREATE INDEX IF NOT EXISTS idx_assignments_policy 
		ON policy_assignments(policy_id);
	
	-- Composite index for active assignment lookups
	CREATE INDEX IF NOT EXISTS idx_assignments_entity_active
		ON policy_assignments(entity_id, effective_from, effective_to);
	
	-- Index for resource type filtering
	CREATE INDEX IF NOT EXISTS idx_policies_resource_type
		ON policies(resource_type);

	-- Employees (entities)
	CREATE TABLE IF NOT EXISTS employees (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT,
		hire_date TEXT NOT NULL,
		created_at TEXT NOT NULL
	);

	-- Snapshots (for period-end balances)
	CREATE TABLE IF NOT EXISTS snapshots (
		id TEXT PRIMARY KEY,
		entity_id TEXT NOT NULL,
		policy_id TEXT NOT NULL,
		period_start TEXT NOT NULL,
		period_end TEXT NOT NULL,
		balance_json TEXT NOT NULL,
		created_at TEXT NOT NULL,
		UNIQUE(entity_id, policy_id, period_start, period_end)
	);

	CREATE INDEX IF NOT EXISTS idx_snapshots_entity_policy 
		ON snapshots(entity_id, policy_id);

	-- Holidays (company-specific and global)
	CREATE TABLE IF NOT EXISTS holidays (
		id TEXT PRIMARY KEY,
		company_id TEXT NOT NULL DEFAULT '',
		date TEXT NOT NULL,
		name TEXT NOT NULL,
		recurring BOOLEAN DEFAULT FALSE,
		created_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_holidays_company_date 
		ON holidays(company_id, date);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_holidays_unique
		ON holidays(company_id, date, name);

	-- Time-off Requests (for approval workflow)
	CREATE TABLE IF NOT EXISTS requests (
		id TEXT PRIMARY KEY,
		entity_id TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		effective_at TEXT NOT NULL,
		amount REAL NOT NULL,
		unit TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		requires_approval BOOLEAN DEFAULT FALSE,
		approved_by TEXT,
		approved_at TEXT,
		rejection_reason TEXT,
		reason TEXT,
		distribution_json TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_requests_entity 
		ON requests(entity_id);
	CREATE INDEX IF NOT EXISTS idx_requests_status 
		ON requests(status);

	-- Reconciliation Runs (for scheduled reconciliation)
	CREATE TABLE IF NOT EXISTS reconciliation_runs (
		id TEXT PRIMARY KEY,
		policy_id TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		period_start TEXT NOT NULL,
		period_end TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		carried_over REAL DEFAULT 0,
		expired REAL DEFAULT 0,
		error TEXT,
		started_at TEXT,
		completed_at TEXT,
		created_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_reconciliation_runs_policy 
		ON reconciliation_runs(policy_id);
	CREATE INDEX IF NOT EXISTS idx_reconciliation_runs_status 
		ON reconciliation_runs(status);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_reconciliation_runs_unique
		ON reconciliation_runs(entity_id, policy_id, period_start, period_end);
	`

	_, err := s.db.Exec(schema)
	return err
}

// =============================================================================
// TRANSACTION STORE (generic.Store interface)
// =============================================================================

// Append adds a transaction to the ledger.
func (s *Store) Append(ctx context.Context, tx generic.Transaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.appendTx(ctx, s.db, tx)
}

func (s *Store) appendTx(ctx context.Context, db interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}, tx generic.Transaction) error {
	metadataJSON, _ := json.Marshal(tx.Metadata)

	query := `
		INSERT INTO transactions 
		(id, entity_id, policy_id, resource_type, effective_at, delta_value, delta_unit, 
		 tx_type, reference_id, reason, idempotency_key, metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := db.ExecContext(ctx, query,
		tx.ID,
		tx.EntityID,
		tx.PolicyID,
		tx.ResourceType.ResourceID(), // Store as string
		tx.EffectiveAt.Time.Format(time.RFC3339),
		tx.Delta.Value.String(),
		tx.Delta.Unit,
		tx.Type,
		tx.ReferenceID,
		tx.Reason,
		nullString(tx.IdempotencyKey),
		string(metadataJSON),
		time.Now().UTC().Format(time.RFC3339),
	)

	if err != nil {
		if isUniqueConstraintError(err) {
			// Distinguish between idempotency key and day uniqueness violations
			if isDayUniquenessError(err) {
				return generic.ErrDuplicateDayConsumption
			}
			return generic.ErrDuplicateIdempotencyKey
		}
		return fmt.Errorf("failed to append transaction: %w", err)
	}

	return nil
}

// AppendBatch adds multiple transactions atomically.
func (s *Store) AppendBatch(ctx context.Context, txs []generic.Transaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate idempotency keys within the batch first
	idempotencyKeys := make(map[string]bool)
	for _, tx := range txs {
		if tx.IdempotencyKey != "" {
			if idempotencyKeys[tx.IdempotencyKey] {
				return generic.ErrDuplicateIdempotencyKey
			}
			idempotencyKeys[tx.IdempotencyKey] = true
		}
	}

	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer sqlTx.Rollback()

	for _, tx := range txs {
		if err := s.appendTx(ctx, sqlTx, tx); err != nil {
			return err
		}
	}

	return sqlTx.Commit()
}

// Load returns all transactions for an entity+policy.
func (s *Store) Load(ctx context.Context, entityID generic.EntityID, policyID generic.PolicyID) ([]generic.Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, entity_id, policy_id, resource_type, effective_at, delta_value, delta_unit,
		       tx_type, reference_id, reason, idempotency_key, metadata_json, created_at
		FROM transactions
		WHERE entity_id = ? AND policy_id = ?
		ORDER BY effective_at ASC, created_at ASC
	`

	return s.queryTransactions(ctx, query, entityID, policyID)
}

// LoadRange returns transactions in a time range.
func (s *Store) LoadRange(ctx context.Context, entityID generic.EntityID, policyID generic.PolicyID, from, to generic.TimePoint) ([]generic.Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, entity_id, policy_id, resource_type, effective_at, delta_value, delta_unit,
		       tx_type, reference_id, reason, idempotency_key, metadata_json, created_at
		FROM transactions
		WHERE entity_id = ? AND policy_id = ? 
		  AND effective_at >= ? AND effective_at <= ?
		ORDER BY effective_at ASC, created_at ASC
	`

	return s.queryTransactions(ctx, query, entityID, policyID,
		from.Time.Format(time.RFC3339), to.Time.Format(time.RFC3339))
}

// Exists checks if an idempotency key exists.
func (s *Store) Exists(ctx context.Context, idempotencyKey string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM transactions WHERE idempotency_key = ?",
		idempotencyKey,
	).Scan(&count)

	return count > 0, err
}

func (s *Store) queryTransactions(ctx context.Context, query string, args ...any) ([]generic.Transaction, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}
	defer rows.Close()

	var transactions []generic.Transaction
	for rows.Next() {
		tx, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, tx)
	}

	return transactions, rows.Err()
}

func scanTransaction(rows *sql.Rows) (generic.Transaction, error) {
	var (
		tx                generic.Transaction
		effectiveAt       string
		resourceTypeID    string // Scan as string, convert to interface
		deltaValue        string
		deltaUnit         string
		referenceID       sql.NullString
		reason            sql.NullString
		idempotencyKey    sql.NullString
		metadataJSON      sql.NullString
		createdAt         string
	)

	err := rows.Scan(
		&tx.ID, &tx.EntityID, &tx.PolicyID, &resourceTypeID,
		&effectiveAt, &deltaValue, &deltaUnit, &tx.Type,
		&referenceID, &reason, &idempotencyKey, &metadataJSON, &createdAt,
	)
	if err != nil {
		return tx, fmt.Errorf("failed to scan transaction: %w", err)
	}

	// Convert string to ResourceType via registry
	tx.ResourceType = generic.GetOrCreateResource(resourceTypeID)
	t, _ := time.Parse(time.RFC3339, effectiveAt)
	tx.EffectiveAt = generic.TimePoint{Time: t}
	tx.Delta = parseAmount(deltaValue, deltaUnit)
	tx.ReferenceID = referenceID.String
	tx.Reason = reason.String
	tx.IdempotencyKey = idempotencyKey.String

	if metadataJSON.Valid && metadataJSON.String != "" {
		json.Unmarshal([]byte(metadataJSON.String), &tx.Metadata)
	}

	return tx, nil
}

// =============================================================================
// TRANSACTIONAL STORE (generic.TxStore interface)
// =============================================================================

// WithTx executes a function within a database transaction.
func (s *Store) WithTx(ctx context.Context, fn func(store generic.Store) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer sqlTx.Rollback()

	txStore := &txStore{tx: sqlTx, parent: s}
	if err := fn(txStore); err != nil {
		return err
	}

	return sqlTx.Commit()
}

type txStore struct {
	tx     *sql.Tx
	parent *Store
}

func (ts *txStore) Append(ctx context.Context, tx generic.Transaction) error {
	return ts.parent.appendTx(ctx, ts.tx, tx)
}

func (ts *txStore) AppendBatch(ctx context.Context, txs []generic.Transaction) error {
	for _, tx := range txs {
		if err := ts.parent.appendTx(ctx, ts.tx, tx); err != nil {
			return err
		}
	}
	return nil
}

func (ts *txStore) Load(ctx context.Context, entityID generic.EntityID, policyID generic.PolicyID) ([]generic.Transaction, error) {
	return ts.parent.Load(ctx, entityID, policyID)
}

func (ts *txStore) LoadRange(ctx context.Context, entityID generic.EntityID, policyID generic.PolicyID, from, to generic.TimePoint) ([]generic.Transaction, error) {
	return ts.parent.LoadRange(ctx, entityID, policyID, from, to)
}

func (ts *txStore) Exists(ctx context.Context, idempotencyKey string) (bool, error) {
	return ts.parent.Exists(ctx, idempotencyKey)
}

// =============================================================================
// POLICY STORE
// =============================================================================

// PolicyRecord is a stored policy with its JSON config.
type PolicyRecord struct {
	ID           string
	Name         string
	ResourceType string
	ConfigJSON   string
	Version      int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// SavePolicy saves a policy record.
func (s *Store) SavePolicy(ctx context.Context, policy PolicyRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
		INSERT INTO policies (id, name, resource_type, config_json, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			resource_type = excluded.resource_type,
			config_json = excluded.config_json,
			version = policies.version + 1,
			updated_at = excluded.updated_at
	`

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, query,
		policy.ID, policy.Name, policy.ResourceType, policy.ConfigJSON,
		policy.Version, now, now,
	)
	return err
}

// GetPolicy retrieves a policy by ID.
func (s *Store) GetPolicy(ctx context.Context, id string) (*PolicyRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var p PolicyRecord
	var createdAt, updatedAt string

	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, resource_type, config_json, version, created_at, updated_at FROM policies WHERE id = ?",
		id,
	).Scan(&p.ID, &p.Name, &p.ResourceType, &p.ConfigJSON, &p.Version, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &p, nil
}

// ListPolicies returns all policies.
func (s *Store) ListPolicies(ctx context.Context) ([]PolicyRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, resource_type, config_json, version, created_at, updated_at FROM policies ORDER BY name",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var policies []PolicyRecord
	for rows.Next() {
		var p PolicyRecord
		var createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.ResourceType, &p.ConfigJSON, &p.Version, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		policies = append(policies, p)
	}
	return policies, rows.Err()
}

// DeletePolicy removes a policy.
func (s *Store) DeletePolicy(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, "DELETE FROM policies WHERE id = ?", id)
	return err
}

// =============================================================================
// EMPLOYEE STORE
// =============================================================================

// Employee represents an employee record.
type Employee struct {
	ID        string
	Name      string
	Email     string
	HireDate  time.Time
	CreatedAt time.Time
}

// SaveEmployee saves an employee.
func (s *Store) SaveEmployee(ctx context.Context, emp Employee) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
		INSERT INTO employees (id, name, email, hire_date, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			email = excluded.email,
			hire_date = excluded.hire_date
	`

	_, err := s.db.ExecContext(ctx, query,
		emp.ID, emp.Name, emp.Email,
		emp.HireDate.Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

// GetEmployee retrieves an employee by ID.
func (s *Store) GetEmployee(ctx context.Context, id string) (*Employee, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var emp Employee
	var hireDate, createdAt string

	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, email, hire_date, created_at FROM employees WHERE id = ?",
		id,
	).Scan(&emp.ID, &emp.Name, &emp.Email, &hireDate, &createdAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	emp.HireDate, _ = time.Parse(time.RFC3339, hireDate)
	emp.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &emp, nil
}

// ListEmployees returns all employees.
func (s *Store) ListEmployees(ctx context.Context) ([]Employee, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, email, hire_date, created_at FROM employees ORDER BY name",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var employees []Employee
	for rows.Next() {
		var emp Employee
		var hireDate, createdAt string
		if err := rows.Scan(&emp.ID, &emp.Name, &emp.Email, &hireDate, &createdAt); err != nil {
			return nil, err
		}
		emp.HireDate, _ = time.Parse(time.RFC3339, hireDate)
		emp.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		employees = append(employees, emp)
	}
	return employees, rows.Err()
}

// DeleteEmployee removes an employee.
func (s *Store) DeleteEmployee(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, "DELETE FROM employees WHERE id = ?", id)
	return err
}

// =============================================================================
// ASSIGNMENT STORE (generic.AssignmentStore interface)
// =============================================================================

// AssignmentRecord is a stored policy assignment.
type AssignmentRecord struct {
	ID                 string
	EntityID           string
	PolicyID           string
	EffectiveFrom      time.Time
	EffectiveTo        *time.Time
	ConsumptionPriority int
	ApprovalConfigJSON string
	CreatedAt          time.Time
}

// SaveAssignment saves a policy assignment.
func (s *Store) SaveAssignment(ctx context.Context, a AssignmentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var effectiveTo *string
	if a.EffectiveTo != nil {
		t := a.EffectiveTo.Format(time.RFC3339)
		effectiveTo = &t
	}

	query := `
		INSERT INTO policy_assignments 
		(id, entity_id, policy_id, effective_from, effective_to, consumption_priority, approval_config_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			effective_from = excluded.effective_from,
			effective_to = excluded.effective_to,
			consumption_priority = excluded.consumption_priority,
			approval_config_json = excluded.approval_config_json
	`

	_, err := s.db.ExecContext(ctx, query,
		a.ID, a.EntityID, a.PolicyID,
		a.EffectiveFrom.Format(time.RFC3339),
		effectiveTo,
		a.ConsumptionPriority,
		a.ApprovalConfigJSON,
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

// GetAssignmentsByEntity returns all assignments for an entity.
func (s *Store) GetAssignmentsByEntity(ctx context.Context, entityID string) ([]AssignmentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, entity_id, policy_id, effective_from, effective_to, 
		       consumption_priority, approval_config_json, created_at
		FROM policy_assignments
		WHERE entity_id = ?
		ORDER BY consumption_priority ASC
	`

	return s.queryAssignments(ctx, query, entityID)
}

// GetAssignmentsByPolicy returns all assignments for a policy.
func (s *Store) GetAssignmentsByPolicy(ctx context.Context, policyID string) ([]AssignmentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, entity_id, policy_id, effective_from, effective_to, 
		       consumption_priority, approval_config_json, created_at
		FROM policy_assignments
		WHERE policy_id = ?
		ORDER BY entity_id
	`

	return s.queryAssignments(ctx, query, policyID)
}

func (s *Store) queryAssignments(ctx context.Context, query string, args ...any) ([]AssignmentRecord, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var assignments []AssignmentRecord
	for rows.Next() {
		var a AssignmentRecord
		var effectiveFrom, createdAt string
		var effectiveTo, approvalConfig sql.NullString

		if err := rows.Scan(&a.ID, &a.EntityID, &a.PolicyID, &effectiveFrom, &effectiveTo,
			&a.ConsumptionPriority, &approvalConfig, &createdAt); err != nil {
			return nil, err
		}

		a.EffectiveFrom, _ = time.Parse(time.RFC3339, effectiveFrom)
		a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if effectiveTo.Valid {
			t, _ := time.Parse(time.RFC3339, effectiveTo.String)
			a.EffectiveTo = &t
		}
		a.ApprovalConfigJSON = approvalConfig.String

		assignments = append(assignments, a)
	}
	return assignments, rows.Err()
}

// DeleteAssignment removes an assignment.
func (s *Store) DeleteAssignment(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, "DELETE FROM policy_assignments WHERE id = ?", id)
	return err
}

// =============================================================================
// SNAPSHOT STORE
// =============================================================================

// SnapshotRecord is a stored balance snapshot.
type SnapshotRecord struct {
	ID          string
	EntityID    string
	PolicyID    string
	PeriodStart time.Time
	PeriodEnd   time.Time
	BalanceJSON string
	CreatedAt   time.Time
}

// SaveSnapshot saves a balance snapshot.
func (s *Store) SaveSnapshot(ctx context.Context, snap SnapshotRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
		INSERT INTO snapshots (id, entity_id, policy_id, period_start, period_end, balance_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(entity_id, policy_id, period_start, period_end) DO UPDATE SET
			balance_json = excluded.balance_json,
			created_at = excluded.created_at
	`

	_, err := s.db.ExecContext(ctx, query,
		snap.ID, snap.EntityID, snap.PolicyID,
		snap.PeriodStart.Format(time.RFC3339),
		snap.PeriodEnd.Format(time.RFC3339),
		snap.BalanceJSON,
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

// GetSnapshot retrieves a snapshot for entity+policy+period.
func (s *Store) GetSnapshot(ctx context.Context, entityID, policyID string, periodStart, periodEnd time.Time) (*SnapshotRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var snap SnapshotRecord
	var start, end, createdAt string

	err := s.db.QueryRowContext(ctx,
		`SELECT id, entity_id, policy_id, period_start, period_end, balance_json, created_at 
		 FROM snapshots WHERE entity_id = ? AND policy_id = ? AND period_start = ? AND period_end = ?`,
		entityID, policyID, periodStart.Format(time.RFC3339), periodEnd.Format(time.RFC3339),
	).Scan(&snap.ID, &snap.EntityID, &snap.PolicyID, &start, &end, &snap.BalanceJSON, &createdAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	snap.PeriodStart, _ = time.Parse(time.RFC3339, start)
	snap.PeriodEnd, _ = time.Parse(time.RFC3339, end)
	snap.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &snap, nil
}

// =============================================================================
// UTILITIES
// =============================================================================

// Reset clears all data (for testing/demo).
func (s *Store) Reset(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tables := []string{"transactions", "snapshots", "policy_assignments", "employees", "policies"}
	for _, table := range tables {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
	}
	return nil
}

// GetAllTransactions returns all transactions (for admin view).
func (s *Store) GetAllTransactions(ctx context.Context, limit int) ([]generic.Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, entity_id, policy_id, resource_type, effective_at, delta_value, delta_unit,
		       tx_type, reference_id, reason, idempotency_key, metadata_json, created_at
		FROM transactions
		ORDER BY created_at DESC
		LIMIT ?
	`

	return s.queryTransactions(ctx, query, limit)
}

// GetTransaction returns a specific transaction by ID.
func (s *Store) GetTransaction(ctx context.Context, id string) (*generic.Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, entity_id, policy_id, resource_type, effective_at, delta_value, delta_unit,
		       tx_type, reference_id, reason, idempotency_key, metadata_json, created_at
		FROM transactions
		WHERE id = ?
	`

	txs, err := s.queryTransactions(ctx, query, id)
	if err != nil {
		return nil, err
	}
	if len(txs) == 0 {
		return nil, nil
	}
	return &txs[0], nil
}

// IsTransactionReversed checks if a transaction has already been reversed.
func (s *Store) IsTransactionReversed(ctx context.Context, txID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT COUNT(*) FROM transactions
		WHERE reference_id = ? AND tx_type = 'reversal'
	`

	var count int
	err := s.db.QueryRowContext(ctx, query, txID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// =============================================================================
// ENTITY-WIDE QUERIES (for TimeOffLedger)
// =============================================================================

// LoadByEntity returns all transactions for an entity across ALL policies.
// This is needed for time-off uniqueness validation.
func (s *Store) LoadByEntity(ctx context.Context, entityID generic.EntityID, from, to generic.TimePoint) ([]generic.Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, entity_id, policy_id, resource_type, effective_at, delta_value, delta_unit,
		       tx_type, reference_id, reason, idempotency_key, metadata_json, created_at
		FROM transactions
		WHERE entity_id = ? 
		  AND effective_at >= ? AND effective_at <= ?
		ORDER BY effective_at ASC, created_at ASC
	`

	return s.queryTransactions(ctx, query, entityID,
		from.Time.Format(time.RFC3339), to.Time.Format(time.RFC3339))
}

// LoadByEntityAndResourceType returns transactions for an entity filtered by resource type.
// Useful for checking "is this day already taken as PTO?" without checking sick leave.
func (s *Store) LoadByEntityAndResourceType(ctx context.Context, entityID generic.EntityID, resourceType generic.ResourceType, from, to generic.TimePoint) ([]generic.Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, entity_id, policy_id, resource_type, effective_at, delta_value, delta_unit,
		       tx_type, reference_id, reason, idempotency_key, metadata_json, created_at
		FROM transactions
		WHERE entity_id = ? AND resource_type = ?
		  AND effective_at >= ? AND effective_at <= ?
		ORDER BY effective_at ASC, created_at ASC
	`

	return s.queryTransactions(ctx, query, entityID, resourceType.ResourceID(),
		from.Time.Format(time.RFC3339), to.Time.Format(time.RFC3339))
}

// GetConsumedDays returns all days that have consumption/pending transactions.
// This is the most efficient way to check "what days is this person off?".
func (s *Store) GetConsumedDays(ctx context.Context, entityID generic.EntityID, resourceType generic.ResourceType, from, to generic.TimePoint) ([]time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT DISTINCT DATE(effective_at) as day
		FROM transactions
		WHERE entity_id = ? AND resource_type = ?
		  AND tx_type IN ('consumption', 'pending')
		  AND effective_at >= ? AND effective_at <= ?
		ORDER BY day ASC
	`

	rows, err := s.db.QueryContext(ctx, query, entityID, resourceType.ResourceID(),
		from.Time.Format(time.RFC3339), to.Time.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var days []time.Time
	for rows.Next() {
		var dayStr string
		if err := rows.Scan(&dayStr); err != nil {
			return nil, err
		}
		// Parse date (SQLite DATE() returns YYYY-MM-DD)
		day, _ := time.Parse("2006-01-02", dayStr)
		days = append(days, day)
	}
	return days, rows.Err()
}

// IsDayConsumed checks if a specific day already has a consumption transaction.
// Returns the existing transaction ID if found.
func (s *Store) IsDayConsumed(ctx context.Context, entityID generic.EntityID, resourceType generic.ResourceType, day generic.TimePoint) (bool, generic.TransactionID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get all transactions on this day
	dayStart := day.Time.Truncate(24 * time.Hour)
	dayEnd := dayStart.Add(24 * time.Hour)

	query := `
		SELECT id FROM transactions
		WHERE entity_id = ? AND resource_type = ?
		  AND tx_type IN ('consumption', 'pending')
		  AND effective_at >= ? AND effective_at < ?
		LIMIT 1
	`

	var txID generic.TransactionID
	err := s.db.QueryRowContext(ctx, query, entityID, resourceType.ResourceID(),
		dayStart.Format(time.RFC3339), dayEnd.Format(time.RFC3339)).Scan(&txID)

	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}

	return true, txID, nil
}

// =============================================================================
// HOLIDAY CALENDAR IMPLEMENTATION
// =============================================================================

// SaveHoliday saves a holiday to the database.
func (s *Store) SaveHoliday(ctx context.Context, h generic.Holiday) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
		INSERT INTO holidays (id, company_id, date, name, recurring, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(company_id, date, name) DO UPDATE SET
			recurring = excluded.recurring
	`

	_, err := s.db.ExecContext(ctx, query,
		h.ID,
		h.CompanyID,
		h.Date.Time.Format("2006-01-02"),
		h.Name,
		h.Recurring,
		time.Now().Format(time.RFC3339),
	)
	return err
}

// DeleteHoliday deletes a holiday by ID.
func (s *Store) DeleteHoliday(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, "DELETE FROM holidays WHERE id = ?", id)
	return err
}

// GetHolidays returns all holidays for a company in a given year.
// Includes both company-specific and global holidays.
func (s *Store) GetHolidays(companyID string, year int) []generic.Holiday {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Query for company-specific and global holidays
	query := `
		SELECT id, company_id, date, name, recurring
		FROM holidays
		WHERE (company_id = ? OR company_id = '')
		  AND (
			(recurring = FALSE AND strftime('%Y', date) = ?)
			OR (recurring = TRUE AND strftime('%m-%d', date) BETWEEN '01-01' AND '12-31')
		  )
		ORDER BY date ASC
	`

	rows, err := s.db.Query(query, companyID, fmt.Sprintf("%d", year))
	if err != nil {
		return nil
	}
	defer rows.Close()

	var holidays []generic.Holiday
	for rows.Next() {
		var h generic.Holiday
		var dateStr string
		if err := rows.Scan(&h.ID, &h.CompanyID, &dateStr, &h.Name, &h.Recurring); err != nil {
			continue
		}

		// Parse date
		t, _ := time.Parse("2006-01-02", dateStr)
		// If recurring, adjust year
		if h.Recurring {
			t = time.Date(year, t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		}
		h.Date = generic.TimePoint{Time: t, Granularity: generic.GranularityDay}

		holidays = append(holidays, h)
	}

	return holidays
}

// IsHoliday checks if a date is a holiday for the given company.
func (s *Store) IsHoliday(companyID string, date generic.TimePoint) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dateStr := date.Time.Format("2006-01-02")
	monthDay := date.Time.Format("01-02")

	query := `
		SELECT COUNT(*) FROM holidays
		WHERE (company_id = ? OR company_id = '')
		  AND (
			(recurring = FALSE AND date = ?)
			OR (recurring = TRUE AND strftime('%m-%d', date) = ?)
		  )
	`

	var count int
	err := s.db.QueryRow(query, companyID, dateStr, monthDay).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// GetAllHolidays returns all holidays (for admin UI).
func (s *Store) GetAllHolidays(ctx context.Context, companyID string) ([]generic.Holiday, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, company_id, date, name, recurring
		FROM holidays
		WHERE company_id = ? OR company_id = ''
		ORDER BY date ASC
	`

	rows, err := s.db.QueryContext(ctx, query, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holidays []generic.Holiday
	for rows.Next() {
		var h generic.Holiday
		var dateStr string
		if err := rows.Scan(&h.ID, &h.CompanyID, &dateStr, &h.Name, &h.Recurring); err != nil {
			return nil, err
		}
		t, _ := time.Parse("2006-01-02", dateStr)
		h.Date = generic.TimePoint{Time: t, Granularity: generic.GranularityDay}
		holidays = append(holidays, h)
	}

	return holidays, rows.Err()
}

// =============================================================================
// REQUEST STORE (for approval workflow)
// =============================================================================

// Request represents a time-off request in storage.
type Request struct {
	ID               string
	EntityID         string
	ResourceType     string
	EffectiveAt      time.Time
	Amount           float64
	Unit             string
	Status           string // pending, approved, rejected, cancelled
	RequiresApproval bool
	ApprovedBy       string
	ApprovedAt       *time.Time
	RejectionReason  string
	Reason           string
	DistributionJSON string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// SaveRequest saves a request to the database.
func (s *Store) SaveRequest(ctx context.Context, r Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
		INSERT INTO requests (id, entity_id, resource_type, effective_at, amount, unit, status,
			requires_approval, approved_by, approved_at, rejection_reason, reason, 
			distribution_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			approved_by = excluded.approved_by,
			approved_at = excluded.approved_at,
			rejection_reason = excluded.rejection_reason,
			updated_at = excluded.updated_at
	`

	var approvedAt *string
	if r.ApprovedAt != nil {
		s := r.ApprovedAt.Format(time.RFC3339)
		approvedAt = &s
	}

	_, err := s.db.ExecContext(ctx, query,
		r.ID, r.EntityID, r.ResourceType, r.EffectiveAt.Format(time.RFC3339),
		r.Amount, r.Unit, r.Status, r.RequiresApproval, r.ApprovedBy,
		approvedAt, r.RejectionReason, r.Reason, r.DistributionJSON,
		r.CreatedAt.Format(time.RFC3339), r.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

// GetRequest retrieves a request by ID.
func (s *Store) GetRequest(ctx context.Context, id string) (*Request, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, entity_id, resource_type, effective_at, amount, unit, status,
			requires_approval, approved_by, approved_at, rejection_reason, reason,
			distribution_json, created_at, updated_at
		FROM requests WHERE id = ?
	`

	var r Request
	var effectiveAt, approvedAt, createdAt, updatedAt sql.NullString
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&r.ID, &r.EntityID, &r.ResourceType, &effectiveAt, &r.Amount, &r.Unit,
		&r.Status, &r.RequiresApproval, &r.ApprovedBy, &approvedAt,
		&r.RejectionReason, &r.Reason, &r.DistributionJSON, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	r.EffectiveAt, _ = time.Parse(time.RFC3339, effectiveAt.String)
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
	if approvedAt.Valid {
		t, _ := time.Parse(time.RFC3339, approvedAt.String)
		r.ApprovedAt = &t
	}

	return &r, nil
}

// GetPendingRequests returns all pending requests.
func (s *Store) GetPendingRequests(ctx context.Context) ([]Request, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, entity_id, resource_type, effective_at, amount, unit, status,
			requires_approval, approved_by, approved_at, rejection_reason, reason,
			distribution_json, created_at, updated_at
		FROM requests
		WHERE status = 'pending'
		ORDER BY created_at ASC
	`

	return s.queryRequests(ctx, query)
}

// GetRequestsByEntity returns all requests for an entity.
func (s *Store) GetRequestsByEntity(ctx context.Context, entityID string) ([]Request, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, entity_id, resource_type, effective_at, amount, unit, status,
			requires_approval, approved_by, approved_at, rejection_reason, reason,
			distribution_json, created_at, updated_at
		FROM requests
		WHERE entity_id = ?
		ORDER BY created_at DESC
	`

	return s.queryRequests(ctx, query, entityID)
}

func (s *Store) queryRequests(ctx context.Context, query string, args ...any) ([]Request, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []Request
	for rows.Next() {
		var r Request
		var effectiveAt, approvedAt, createdAt, updatedAt sql.NullString
		if err := rows.Scan(
			&r.ID, &r.EntityID, &r.ResourceType, &effectiveAt, &r.Amount, &r.Unit,
			&r.Status, &r.RequiresApproval, &r.ApprovedBy, &approvedAt,
			&r.RejectionReason, &r.Reason, &r.DistributionJSON, &createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}

		r.EffectiveAt, _ = time.Parse(time.RFC3339, effectiveAt.String)
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
		r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
		if approvedAt.Valid {
			t, _ := time.Parse(time.RFC3339, approvedAt.String)
			r.ApprovedAt = &t
		}

		requests = append(requests, r)
	}

	return requests, rows.Err()
}

// =============================================================================
// RECONCILIATION RUNS STORE
// =============================================================================

// ReconciliationRun represents a scheduled/completed reconciliation.
type ReconciliationRun struct {
	ID          string
	PolicyID    string
	EntityID    string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Status      string // pending, running, completed, failed
	CarriedOver float64
	Expired     float64
	Error       string
	StartedAt   *time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
}

// SaveReconciliationRun saves a reconciliation run.
func (s *Store) SaveReconciliationRun(ctx context.Context, r ReconciliationRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
		INSERT INTO reconciliation_runs (id, policy_id, entity_id, period_start, period_end,
			status, carried_over, expired, error, started_at, completed_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(entity_id, policy_id, period_start, period_end) DO UPDATE SET
			status = excluded.status,
			carried_over = excluded.carried_over,
			expired = excluded.expired,
			error = excluded.error,
			started_at = excluded.started_at,
			completed_at = excluded.completed_at
	`

	var startedAt, completedAt *string
	if r.StartedAt != nil {
		s := r.StartedAt.Format(time.RFC3339)
		startedAt = &s
	}
	if r.CompletedAt != nil {
		s := r.CompletedAt.Format(time.RFC3339)
		completedAt = &s
	}

	_, err := s.db.ExecContext(ctx, query,
		r.ID, r.PolicyID, r.EntityID,
		r.PeriodStart.Format(time.RFC3339), r.PeriodEnd.Format(time.RFC3339),
		r.Status, r.CarriedOver, r.Expired, r.Error,
		startedAt, completedAt, r.CreatedAt.Format(time.RFC3339),
	)
	return err
}

// GetReconciliationRuns returns reconciliation runs.
func (s *Store) GetReconciliationRuns(ctx context.Context, status string) ([]ReconciliationRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var query string
	var args []any

	if status != "" {
		query = `
			SELECT id, policy_id, entity_id, period_start, period_end, status,
				carried_over, expired, error, started_at, completed_at, created_at
			FROM reconciliation_runs
			WHERE status = ?
			ORDER BY created_at DESC
		`
		args = []any{status}
	} else {
		query = `
			SELECT id, policy_id, entity_id, period_start, period_end, status,
				carried_over, expired, error, started_at, completed_at, created_at
			FROM reconciliation_runs
			ORDER BY created_at DESC
		`
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []ReconciliationRun
	for rows.Next() {
		var r ReconciliationRun
		var periodStart, periodEnd, startedAt, completedAt, createdAt sql.NullString
		if err := rows.Scan(
			&r.ID, &r.PolicyID, &r.EntityID, &periodStart, &periodEnd, &r.Status,
			&r.CarriedOver, &r.Expired, &r.Error, &startedAt, &completedAt, &createdAt,
		); err != nil {
			return nil, err
		}

		r.PeriodStart, _ = time.Parse(time.RFC3339, periodStart.String)
		r.PeriodEnd, _ = time.Parse(time.RFC3339, periodEnd.String)
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
		if startedAt.Valid {
			t, _ := time.Parse(time.RFC3339, startedAt.String)
			r.StartedAt = &t
		}
		if completedAt.Valid {
			t, _ := time.Parse(time.RFC3339, completedAt.String)
			r.CompletedAt = &t
		}

		runs = append(runs, r)
	}

	return runs, rows.Err()
}

// IsReconciliationComplete checks if a reconciliation has already been done.
func (s *Store) IsReconciliationComplete(ctx context.Context, entityID, policyID string, periodEnd time.Time) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT COUNT(*) FROM reconciliation_runs
		WHERE entity_id = ? AND policy_id = ? AND period_end = ? AND status = 'completed'
	`

	var count int
	err := s.db.QueryRowContext(ctx, query, entityID, policyID, periodEnd.Format(time.RFC3339)).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Helper functions

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func parseAmount(value, unit string) generic.Amount {
	return generic.Amount{
		Value: generic.MustParseDecimal(value),
		Unit:  generic.Unit(unit),
	}
}

func isUniqueConstraintError(err error) bool {
	return err != nil && (contains(err.Error(), "UNIQUE constraint failed") ||
		contains(err.Error(), "duplicate key"))
}

func isDayUniquenessError(err error) bool {
	return err != nil && contains(err.Error(), "idx_unique_day_consumption")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
