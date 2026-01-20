/*
handlers_test.go - Unit tests for API handlers

Tests for:
- Transaction cancellation (CancelTransaction)
- Balance updates after cancellation
*/
package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/warp/resource-engine/factory"
	"github.com/warp/resource-engine/generic"
	"github.com/warp/resource-engine/store/sqlite"
	"github.com/warp/resource-engine/timeoff"
)

func TestCancelTransaction_Success(t *testing.T) {
	// GIVEN: An employee with a consumption transaction
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create an employee
	emp := sqlite.Employee{
		ID:       "emp-test",
		Name:     "Test User",
		Email:    "test@example.com",
		HireDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := store.SaveEmployee(ctx, emp); err != nil {
		t.Fatalf("Failed to create employee: %v", err)
	}

	// Create a policy
	pf := factory.NewPolicyFactory()
	policyJSON := timeoff.StandardPTOJSON("pto-test", "Test PTO", 24, 5)
	policy, accrual, err := pf.ParsePolicy(policyJSON)
	if err != nil {
		t.Fatalf("Failed to parse policy: %v", err)
	}
	_ = accrual // Not needed for this test

	policyRecord := sqlite.PolicyRecord{
		ID:           string(policy.ID),
		Name:         policy.Name,
		ResourceType: policy.ResourceType.ResourceID(),
		ConfigJSON:   policyJSON,
		Version:      1,
	}
	if err := store.SavePolicy(ctx, policyRecord); err != nil {
		t.Fatalf("Failed to save policy: %v", err)
	}

	// Assign policy
	assign := sqlite.AssignmentRecord{
		ID:                  "assign-test",
		EntityID:            "emp-test",
		PolicyID:            "pto-test",
		EffectiveFrom:       time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		ConsumptionPriority: 1,
	}
	if err := store.SaveAssignment(ctx, assign); err != nil {
		t.Fatalf("Failed to save assignment: %v", err)
	}

	// Create a consumption transaction (one day off)
	consumptionTx := generic.Transaction{
		ID:             "tx-consume-1",
		EntityID:       "emp-test",
		PolicyID:       "pto-test",
		ResourceType:   timeoff.ResourcePTO,
		EffectiveAt:    generic.TimePoint{Time: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)},
		Delta:          generic.NewAmount(-1, generic.UnitDays),
		Type:           generic.TxConsumption,
		Reason:         "Test day off",
		IdempotencyKey: "test-consume-1",
	}
	if err := store.AppendBatch(ctx, []generic.Transaction{consumptionTx}); err != nil {
		t.Fatalf("Failed to create consumption transaction: %v", err)
	}

	// WHEN: Cancelling the transaction
	// Verify the transaction exists
	tx, err := store.GetTransaction(ctx, "tx-consume-1")
	if err != nil {
		t.Fatalf("Failed to get transaction: %v", err)
	}
	if tx == nil {
		t.Fatal("Transaction not found")
	}
	if tx.Type != generic.TxConsumption {
		t.Errorf("Expected TxConsumption, got %v", tx.Type)
	}

	// Check not already reversed
	isReversed, err := store.IsTransactionReversed(ctx, "tx-consume-1")
	if err != nil {
		t.Fatalf("Failed to check reversal: %v", err)
	}
	if isReversed {
		t.Fatal("Transaction should not be reversed yet")
	}

	// Create reversal
	reversalTx := generic.Transaction{
		ID:             "reversal-tx-consume-1",
		EntityID:       tx.EntityID,
		PolicyID:       tx.PolicyID,
		ResourceType:   tx.ResourceType,
		EffectiveAt:    tx.EffectiveAt,
		Delta:          tx.Delta.Neg(),
		Type:           generic.TxReversal,
		ReferenceID:    "tx-consume-1",
		Reason:         "Cancelled by user",
		IdempotencyKey: "reversal-tx-consume-1",
	}
	if err := store.AppendBatch(ctx, []generic.Transaction{reversalTx}); err != nil {
		t.Fatalf("Failed to create reversal: %v", err)
	}

	// THEN: Transaction should be marked as reversed
	isReversed, err = store.IsTransactionReversed(ctx, "tx-consume-1")
	if err != nil {
		t.Fatalf("Failed to check reversal after: %v", err)
	}
	if !isReversed {
		t.Error("Transaction should be reversed")
	}

	// And balance should reflect the reversal
	ledger := generic.NewLedger(store)
	year := 2026
	yearStart := generic.TimePoint{Time: time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)}
	yearEnd := generic.TimePoint{Time: time.Date(year, 12, 31, 23, 59, 59, 0, time.UTC)}

	txs, err := ledger.TransactionsInRange(ctx, "emp-test", "pto-test", yearStart, yearEnd)
	if err != nil {
		t.Fatalf("Failed to get transactions: %v", err)
	}

	// Should have 2 transactions: consumption + reversal
	if len(txs) != 2 {
		t.Errorf("Expected 2 transactions (consumption + reversal), got %d", len(txs))
	}

	// Calculate balance - consumption and reversal should cancel out
	var consumed int
	for _, tx := range txs {
		switch tx.Type {
		case generic.TxConsumption:
			consumed++
		case generic.TxReversal:
			consumed--
		}
	}
	if consumed != 0 {
		t.Errorf("Expected net 0 consumed days after reversal, got %d", consumed)
	}
}

func TestCancelTransaction_AlreadyReversed(t *testing.T) {
	// GIVEN: A transaction that's already been reversed
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create consumption + reversal
	txs := []generic.Transaction{
		{
			ID:             "tx-consume-2",
			EntityID:       "emp-test",
			PolicyID:       "pto-test",
			ResourceType:   timeoff.ResourcePTO,
			EffectiveAt:    generic.TimePoint{Time: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
			Delta:          generic.NewAmount(-1, generic.UnitDays),
			Type:           generic.TxConsumption,
			IdempotencyKey: "test-consume-2",
		},
		{
			ID:             "reversal-tx-consume-2",
			EntityID:       "emp-test",
			PolicyID:       "pto-test",
			ResourceType:   timeoff.ResourcePTO,
			EffectiveAt:    generic.TimePoint{Time: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
			Delta:          generic.NewAmount(1, generic.UnitDays),
			Type:           generic.TxReversal,
			ReferenceID:    "tx-consume-2",
			IdempotencyKey: "reversal-tx-consume-2",
		},
	}
	if err := store.AppendBatch(ctx, txs); err != nil {
		t.Fatalf("Failed to create transactions: %v", err)
	}

	// WHEN: Checking if already reversed
	isReversed, err := store.IsTransactionReversed(ctx, "tx-consume-2")
	if err != nil {
		t.Fatalf("Failed to check reversal: %v", err)
	}

	// THEN: Should be marked as reversed
	if !isReversed {
		t.Error("Transaction should be marked as reversed")
	}
}

func TestCancelTransaction_MultiDayRequest_CancelOne(t *testing.T) {
	// GIVEN: An employee with a 3-day time off request (3 separate transactions)
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create 3 consumption transactions (one per day)
	requestID := "req-12345"
	var txs []generic.Transaction
	for i := 0; i < 3; i++ {
		txs = append(txs, generic.Transaction{
			ID:             generic.TransactionID(fmt.Sprintf("%s-0-%d", requestID, i)),
			EntityID:       "emp-test",
			PolicyID:       "pto-test",
			ResourceType:   timeoff.ResourcePTO,
			EffectiveAt:    generic.TimePoint{Time: time.Date(2026, 4, 10+i, 0, 0, 0, 0, time.UTC)},
			Delta:          generic.NewAmount(-1, generic.UnitDays),
			Type:           generic.TxConsumption,
			ReferenceID:    requestID,
			Reason:         "Spring vacation",
			IdempotencyKey: fmt.Sprintf("%s-0-%d", requestID, i),
		})
	}
	if err := store.AppendBatch(ctx, txs); err != nil {
		t.Fatalf("Failed to create transactions: %v", err)
	}

	// WHEN: Cancel only the middle day (April 11)
	middleTxID := fmt.Sprintf("%s-0-1", requestID)
	
	middleTx, err := store.GetTransaction(ctx, middleTxID)
	if err != nil {
		t.Fatalf("Failed to get middle transaction: %v", err)
	}
	if middleTx == nil {
		t.Fatal("Middle transaction not found")
	}

	// Create reversal for just the middle day
	reversalTx := generic.Transaction{
		ID:             generic.TransactionID(fmt.Sprintf("reversal-%s", middleTxID)),
		EntityID:       middleTx.EntityID,
		PolicyID:       middleTx.PolicyID,
		ResourceType:   middleTx.ResourceType,
		EffectiveAt:    middleTx.EffectiveAt,
		Delta:          middleTx.Delta.Neg(),
		Type:           generic.TxReversal,
		ReferenceID:    middleTxID,
		Reason:         "Cancelled by user",
		IdempotencyKey: fmt.Sprintf("reversal-%s", middleTxID),
	}
	if err := store.AppendBatch(ctx, []generic.Transaction{reversalTx}); err != nil {
		t.Fatalf("Failed to create reversal: %v", err)
	}

	// THEN: Only the middle day should be reversed
	isMiddleReversed, _ := store.IsTransactionReversed(ctx, middleTxID)
	isFirstReversed, _ := store.IsTransactionReversed(ctx, fmt.Sprintf("%s-0-0", requestID))
	isLastReversed, _ := store.IsTransactionReversed(ctx, fmt.Sprintf("%s-0-2", requestID))

	if !isMiddleReversed {
		t.Error("Middle day should be reversed")
	}
	if isFirstReversed {
		t.Error("First day should NOT be reversed")
	}
	if isLastReversed {
		t.Error("Last day should NOT be reversed")
	}

	// Net consumption should be 2 days (3 consumed - 1 reversed)
	ledger := generic.NewLedger(store)
	allTxs, _ := ledger.TransactionsInRange(ctx, "emp-test", "pto-test",
		generic.TimePoint{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		generic.TimePoint{Time: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)})

	var netConsumed float64
	for _, tx := range allTxs {
		switch tx.Type {
		case generic.TxConsumption:
			netConsumed += tx.Delta.Value.InexactFloat64()
		case generic.TxReversal:
			netConsumed += tx.Delta.Value.InexactFloat64()
		}
	}

	// netConsumed should be -2 (3 days consumed, 1 day restored)
	if netConsumed != -2 {
		t.Errorf("Expected net -2 days consumed, got %.1f", netConsumed)
	}
}

// =============================================================================
// HOLIDAY TESTS
// =============================================================================

func TestHoliday_CreateAndQuery(t *testing.T) {
	// GIVEN: A clean store
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// WHEN: Creating a holiday
	holiday := generic.Holiday{
		ID:        "holiday-1",
		CompanyID: "company-1",
		Date:      generic.NewTimePoint(2026, 12, 25),
		Name:      "Christmas Day",
		Recurring: true,
	}
	if err := store.SaveHoliday(ctx, holiday); err != nil {
		t.Fatalf("Failed to save holiday: %v", err)
	}

	// THEN: Holiday should be queryable
	holidays, err := store.GetAllHolidays(ctx, "company-1")
	if err != nil {
		t.Fatalf("Failed to get holidays: %v", err)
	}
	if len(holidays) != 1 {
		t.Errorf("Expected 1 holiday, got %d", len(holidays))
	}
	if holidays[0].Name != "Christmas Day" {
		t.Errorf("Expected 'Christmas Day', got '%s'", holidays[0].Name)
	}

	// And IsHoliday should return true
	isHoliday := store.IsHoliday("company-1", generic.NewTimePoint(2026, 12, 25))
	if !isHoliday {
		t.Error("December 25 should be a holiday")
	}

	// But Dec 26 should not be a holiday
	isHoliday = store.IsHoliday("company-1", generic.NewTimePoint(2026, 12, 26))
	if isHoliday {
		t.Error("December 26 should NOT be a holiday")
	}
}

func TestHoliday_RecurringAcrossYears(t *testing.T) {
	// GIVEN: A recurring holiday
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	holiday := generic.Holiday{
		ID:        "holiday-july4",
		CompanyID: "",
		Date:      generic.NewTimePoint(2025, 7, 4),
		Name:      "Independence Day",
		Recurring: true,
	}
	if err := store.SaveHoliday(ctx, holiday); err != nil {
		t.Fatalf("Failed to save holiday: %v", err)
	}

	// THEN: Should be a holiday in 2025
	if !store.IsHoliday("any-company", generic.NewTimePoint(2025, 7, 4)) {
		t.Error("July 4, 2025 should be a holiday")
	}

	// And in 2026 (recurring)
	if !store.IsHoliday("any-company", generic.NewTimePoint(2026, 7, 4)) {
		t.Error("July 4, 2026 should be a holiday (recurring)")
	}

	// And in 2030
	if !store.IsHoliday("any-company", generic.NewTimePoint(2030, 7, 4)) {
		t.Error("July 4, 2030 should be a holiday (recurring)")
	}
}

func TestHoliday_Delete(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a holiday
	holiday := generic.Holiday{
		ID:        "holiday-to-delete",
		CompanyID: "company-1",
		Date:      generic.NewTimePoint(2026, 1, 1),
		Name:      "New Year",
		Recurring: false,
	}
	store.SaveHoliday(ctx, holiday)

	// Verify it exists
	if !store.IsHoliday("company-1", generic.NewTimePoint(2026, 1, 1)) {
		t.Fatal("Holiday should exist before deletion")
	}

	// Delete it
	if err := store.DeleteHoliday(ctx, "holiday-to-delete"); err != nil {
		t.Fatalf("Failed to delete holiday: %v", err)
	}

	// Should no longer exist
	if store.IsHoliday("company-1", generic.NewTimePoint(2026, 1, 1)) {
		t.Error("Holiday should not exist after deletion")
	}
}

// =============================================================================
// REQUEST APPROVAL TESTS
// =============================================================================

func TestRequest_SaveAndQuery(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a pending request
	req := sqlite.Request{
		ID:               "req-123",
		EntityID:         "emp-1",
		ResourceType:     "pto",
		EffectiveAt:      time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		Amount:           2.0,
		Unit:             "days",
		Status:           "pending",
		RequiresApproval: true,
		Reason:           "Vacation",
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := store.SaveRequest(ctx, req); err != nil {
		t.Fatalf("Failed to save request: %v", err)
	}

	// Query by ID
	fetched, err := store.GetRequest(ctx, "req-123")
	if err != nil {
		t.Fatalf("Failed to get request: %v", err)
	}
	if fetched == nil {
		t.Fatal("Request not found")
	}
	if fetched.Status != "pending" {
		t.Errorf("Expected status 'pending', got '%s'", fetched.Status)
	}

	// Query pending requests
	pending, err := store.GetPendingRequests(ctx)
	if err != nil {
		t.Fatalf("Failed to get pending requests: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("Expected 1 pending request, got %d", len(pending))
	}
}

func TestRequest_ApproveUpdatesStatus(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a pending request
	now := time.Now()
	req := sqlite.Request{
		ID:               "req-approve",
		EntityID:         "emp-1",
		ResourceType:     "pto",
		EffectiveAt:      time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Amount:           1.0,
		Unit:             "days",
		Status:           "pending",
		RequiresApproval: true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	store.SaveRequest(ctx, req)

	// Approve it
	approveTime := time.Now()
	req.Status = "approved"
	req.ApprovedBy = "manager-1"
	req.ApprovedAt = &approveTime
	req.UpdatedAt = approveTime
	store.SaveRequest(ctx, req)

	// Verify
	fetched, _ := store.GetRequest(ctx, "req-approve")
	if fetched.Status != "approved" {
		t.Errorf("Expected status 'approved', got '%s'", fetched.Status)
	}
	if fetched.ApprovedBy != "manager-1" {
		t.Errorf("Expected approver 'manager-1', got '%s'", fetched.ApprovedBy)
	}

	// Should not appear in pending
	pending, _ := store.GetPendingRequests(ctx)
	if len(pending) != 0 {
		t.Errorf("Expected 0 pending requests, got %d", len(pending))
	}
}

func TestRequest_RejectUpdatesStatus(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a pending request
	now := time.Now()
	req := sqlite.Request{
		ID:               "req-reject",
		EntityID:         "emp-1",
		ResourceType:     "pto",
		EffectiveAt:      time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Amount:           5.0,
		Unit:             "days",
		Status:           "pending",
		RequiresApproval: true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	store.SaveRequest(ctx, req)

	// Reject it
	req.Status = "rejected"
	req.RejectionReason = "Insufficient coverage"
	req.UpdatedAt = time.Now()
	store.SaveRequest(ctx, req)

	// Verify
	fetched, _ := store.GetRequest(ctx, "req-reject")
	if fetched.Status != "rejected" {
		t.Errorf("Expected status 'rejected', got '%s'", fetched.Status)
	}
	if fetched.RejectionReason != "Insufficient coverage" {
		t.Errorf("Expected reason 'Insufficient coverage', got '%s'", fetched.RejectionReason)
	}
}

// =============================================================================
// RECONCILIATION RUN TESTS
// =============================================================================

func TestReconciliationRun_SaveAndQuery(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a completed run
	completedAt := time.Now()
	run := sqlite.ReconciliationRun{
		ID:          "run-1",
		PolicyID:    "pto-standard",
		EntityID:    "emp-1",
		PeriodStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
		Status:      "completed",
		CarriedOver: 5.0,
		Expired:     10.0,
		CompletedAt: &completedAt,
		CreatedAt:   time.Now(),
	}

	if err := store.SaveReconciliationRun(ctx, run); err != nil {
		t.Fatalf("Failed to save run: %v", err)
	}

	// Query all runs
	runs, err := store.GetReconciliationRuns(ctx, "")
	if err != nil {
		t.Fatalf("Failed to get runs: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("Expected 1 run, got %d", len(runs))
	}

	// Query completed runs
	completed, _ := store.GetReconciliationRuns(ctx, "completed")
	if len(completed) != 1 {
		t.Errorf("Expected 1 completed run, got %d", len(completed))
	}

	// Query pending runs (should be 0)
	pending, _ := store.GetReconciliationRuns(ctx, "pending")
	if len(pending) != 0 {
		t.Errorf("Expected 0 pending runs, got %d", len(pending))
	}
}

func TestReconciliationRun_IsComplete(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	periodEnd := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)

	// Check before any runs
	isComplete, err := store.IsReconciliationComplete(ctx, "emp-1", "pto-1", periodEnd)
	if err != nil {
		t.Fatalf("Failed to check: %v", err)
	}
	if isComplete {
		t.Error("Should not be complete before any runs")
	}

	// Create a completed run
	completedAt := time.Now()
	run := sqlite.ReconciliationRun{
		ID:          "run-2",
		PolicyID:    "pto-1",
		EntityID:    "emp-1",
		PeriodStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   periodEnd,
		Status:      "completed",
		CompletedAt: &completedAt,
		CreatedAt:   time.Now(),
	}
	store.SaveReconciliationRun(ctx, run)

	// Check after completion
	isComplete, _ = store.IsReconciliationComplete(ctx, "emp-1", "pto-1", periodEnd)
	if !isComplete {
		t.Error("Should be complete after run")
	}

	// Different employee should not be complete
	isComplete, _ = store.IsReconciliationComplete(ctx, "emp-2", "pto-1", periodEnd)
	if isComplete {
		t.Error("Different employee should not be complete")
	}
}
