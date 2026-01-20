/*
scheduler.go - Automated reconciliation scheduler

PURPOSE:
  Periodically checks for policy assignments that need reconciliation
  (year-end rollover/expiration) and automatically processes them.

DESIGN:
  - Runs a background goroutine with configurable check interval
  - Detects assignments where current date is past the period end
  - Skips assignments that have already been reconciled
  - Records reconciliation runs for audit and UI display

CONFIGURATION:
  - CheckInterval: How often to check (default: 1 hour)
  - Enabled: Whether scheduler is active (default: true)

USAGE:
  scheduler := NewReconciliationScheduler(store, handler)
  scheduler.Start()
  // ... later
  scheduler.Stop()

SEE ALSO:
  - handlers.go: TriggerRollover endpoint (manual reconciliation)
  - generic/policy.go: ReconciliationEngine
*/
package api

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/warp/resource-engine/generic"
	"github.com/warp/resource-engine/store/sqlite"
)

// ReconciliationScheduler handles automated year-end reconciliation.
type ReconciliationScheduler struct {
	Store         *sqlite.Store
	Handler       *Handler
	CheckInterval time.Duration
	Enabled       bool

	ticker *time.Ticker
	stop   chan bool
	wg     sync.WaitGroup
	mu     sync.Mutex
}

// NewReconciliationScheduler creates a new scheduler.
func NewReconciliationScheduler(store *sqlite.Store, handler *Handler) *ReconciliationScheduler {
	return &ReconciliationScheduler{
		Store:         store,
		Handler:       handler,
		CheckInterval: 1 * time.Hour,
		Enabled:       true,
		stop:          make(chan bool),
	}
}

// Start begins the scheduler.
func (rs *ReconciliationScheduler) Start() {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if !rs.Enabled {
		log.Println("[Scheduler] Disabled, not starting")
		return
	}

	rs.ticker = time.NewTicker(rs.CheckInterval)
	rs.wg.Add(1)

	go rs.run()

	log.Printf("[Scheduler] Started with check interval: %v", rs.CheckInterval)
}

// Stop stops the scheduler.
func (rs *ReconciliationScheduler) Stop() {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.ticker != nil {
		rs.ticker.Stop()
		close(rs.stop)
		rs.wg.Wait()
		log.Println("[Scheduler] Stopped")
	}
}

func (rs *ReconciliationScheduler) run() {
	defer rs.wg.Done()

	// Run immediately on start
	rs.checkAndProcess()

	for {
		select {
		case <-rs.ticker.C:
			rs.checkAndProcess()
		case <-rs.stop:
			return
		}
	}
}

func (rs *ReconciliationScheduler) checkAndProcess() {
	ctx := context.Background()
	now := time.Now()

	log.Printf("[Scheduler] Checking for reconciliations at %v", now)

	// Get all active assignments
	employees, err := rs.Store.ListEmployees(ctx)
	if err != nil {
		log.Printf("[Scheduler] Error listing employees: %v", err)
		return
	}

	processedCount := 0
	skippedCount := 0

	for _, emp := range employees {
		assignments, err := rs.Store.GetAssignmentsByEntity(ctx, emp.ID)
		if err != nil {
			log.Printf("[Scheduler] Error getting assignments for %s: %v", emp.ID, err)
			continue
		}

		for _, assign := range assignments {
			// Get the policy to determine period
			policyRecord, err := rs.Store.GetPolicy(ctx, assign.PolicyID)
			if err != nil || policyRecord == nil {
				continue
			}

			// Parse the policy config
			policy, _, err := rs.Handler.PolicyFactory.ParsePolicy(policyRecord.ConfigJSON)
			if err != nil {
				continue
			}

			// Calculate the period for this policy
			period := policy.PeriodConfig.PeriodFor(generic.Today())

			// Check if we're past the period end
			if !now.After(period.End.Time) {
				// Current period not ended yet
				continue
			}

			// Check if reconciliation already done for this period
			alreadyDone, err := rs.Store.IsReconciliationComplete(ctx, emp.ID, assign.PolicyID, period.End.Time)
			if err != nil {
				log.Printf("[Scheduler] Error checking reconciliation status: %v", err)
				continue
			}
			if alreadyDone {
				skippedCount++
				continue
			}

			// Process reconciliation
			err = rs.processReconciliation(ctx, emp.ID, assign, policy, period)
			if err != nil {
				log.Printf("[Scheduler] Error processing reconciliation for %s/%s: %v", emp.ID, assign.PolicyID, err)
			} else {
				processedCount++
			}
		}
	}

	if processedCount > 0 || skippedCount > 0 {
		log.Printf("[Scheduler] Completed: %d processed, %d skipped (already done)", processedCount, skippedCount)
	}
}

func (rs *ReconciliationScheduler) processReconciliation(
	ctx context.Context,
	entityID string,
	assign sqlite.AssignmentRecord,
	policy *generic.Policy,
	period generic.Period,
) error {
	runID := fmt.Sprintf("run-%d", time.Now().UnixNano())
	startTime := time.Now()

	// Create run record (pending)
	run := sqlite.ReconciliationRun{
		ID:          runID,
		PolicyID:    assign.PolicyID,
		EntityID:    entityID,
		PeriodStart: period.Start.Time,
		PeriodEnd:   period.End.Time,
		Status:      "running",
		StartedAt:   &startTime,
		CreatedAt:   startTime,
	}

	if err := rs.Store.SaveReconciliationRun(ctx, run); err != nil {
		return fmt.Errorf("failed to save run record: %w", err)
	}

	// Calculate current balance
	txs, err := rs.Store.LoadRange(ctx, generic.EntityID(entityID), generic.PolicyID(assign.PolicyID), period.Start, period.End)
	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
		rs.Store.SaveReconciliationRun(ctx, run)
		return err
	}

	// Get accrual schedule if available
	var accruals generic.AccrualSchedule
	if sched, ok := rs.Handler.accruals[generic.PolicyID(assign.PolicyID)]; ok {
		accruals = sched
	}

	// Calculate balance from transactions
	balance := calculateBalanceForScheduler(txs, period, policy.Unit, accruals, period.End)

	// Process reconciliation
	engine := &generic.ReconciliationEngine{}
	nextPeriod := policy.PeriodConfig.PeriodFor(period.End.AddDays(1))

	output, err := engine.Process(generic.ReconciliationInput{
		EntityID:       generic.EntityID(entityID),
		PolicyID:       generic.PolicyID(assign.PolicyID),
		Policy:         *policy,
		CurrentBalance: balance,
		EndingPeriod:   period,
		NextPeriod:     nextPeriod,
	})

	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
		rs.Store.SaveReconciliationRun(ctx, run)
		return err
	}

	// Append reconciliation transactions
	if len(output.Transactions) > 0 {
		if err := rs.Store.AppendBatch(ctx, output.Transactions); err != nil {
			run.Status = "failed"
			run.Error = err.Error()
			rs.Store.SaveReconciliationRun(ctx, run)
			return err
		}
	}

	// Update run record (completed)
	completedTime := time.Now()
	run.Status = "completed"
	carriedOver, _ := output.Summary.CarriedOver.Value.Float64()
	expired, _ := output.Summary.Expired.Value.Float64()
	run.CarriedOver = carriedOver
	run.Expired = expired
	run.CompletedAt = &completedTime

	if err := rs.Store.SaveReconciliationRun(ctx, run); err != nil {
		return fmt.Errorf("failed to update run record: %w", err)
	}

	log.Printf("[Scheduler] Processed %s/%s: carried=%.2f, expired=%.2f",
		entityID, assign.PolicyID, carriedOver, expired)

	return nil
}

// RunNow triggers an immediate check (for testing/admin).
func (rs *ReconciliationScheduler) RunNow() {
	rs.checkAndProcess()
}

// GetNextRunTime returns when the next scheduled check will occur.
func (rs *ReconciliationScheduler) GetNextRunTime() time.Time {
	return time.Now().Add(rs.CheckInterval)
}

// calculateBalanceForScheduler calculates balance for reconciliation.
// This mirrors the calculateBalance function in handlers.go.
func calculateBalanceForScheduler(txs []generic.Transaction, period generic.Period, unit generic.Unit, accrual generic.AccrualSchedule, asOf generic.TimePoint) generic.Balance {
	var actualAccruals, consumed, pending, adjustments generic.Amount
	actualAccruals = generic.NewAmount(0, unit)
	consumed = generic.NewAmount(0, unit)
	pending = generic.NewAmount(0, unit)
	adjustments = generic.NewAmount(0, unit)

	for _, tx := range txs {
		switch tx.Type {
		case generic.TxGrant:
			actualAccruals = actualAccruals.Add(tx.Delta)
		case generic.TxConsumption:
			consumed = consumed.Add(tx.Delta.Neg())
		case generic.TxPending:
			pending = pending.Add(tx.Delta.Neg())
		case generic.TxReconciliation, generic.TxAdjustment:
			adjustments = adjustments.Add(tx.Delta)
		case generic.TxReversal:
			consumed = consumed.Sub(tx.Delta)
		}
	}

	// Calculate scheduled accruals if deterministic
	var scheduledAccruals generic.Amount
	scheduledAccruals = generic.NewAmount(0, unit)
	if accrual != nil && accrual.IsDeterministic() {
		events := accrual.GenerateAccruals(period.Start, asOf)
		for _, e := range events {
			scheduledAccruals = scheduledAccruals.Add(e.Amount)
		}
	}

	// Use maximum of actual and scheduled
	accruedToDate := actualAccruals
	if scheduledAccruals.GreaterThan(actualAccruals) {
		accruedToDate = scheduledAccruals
	}

	// Full entitlement for deterministic
	totalEntitlement := accruedToDate
	if accrual != nil && accrual.IsDeterministic() {
		events := accrual.GenerateAccruals(period.Start, period.End)
		totalEntitlement = generic.NewAmount(0, unit)
		for _, e := range events {
			totalEntitlement = totalEntitlement.Add(e.Amount)
		}
	}

	return generic.Balance{
		AccruedToDate:    accruedToDate,
		TotalEntitlement: totalEntitlement,
		TotalConsumed:    consumed,
		Pending:          pending,
		Adjustments:      adjustments,
	}
}
