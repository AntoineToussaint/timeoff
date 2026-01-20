package rewards_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/warp/resource-engine/generic"
	"github.com/warp/resource-engine/rewards"
	"github.com/warp/resource-engine/store/sqlite"
)

// =============================================================================
// TEST HELPERS
// =============================================================================

func newTestStore(t *testing.T) *sqlite.Store {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func newTestLedger(store *sqlite.Store) generic.Ledger {
	return generic.NewLedger(store)
}

func points(n float64) generic.Amount {
	return generic.NewAmount(n, rewards.UnitPoints)
}

func dollars(n float64) generic.Amount {
	return generic.NewAmount(n, rewards.UnitDollars)
}

func hours(n float64) generic.Amount {
	return generic.NewAmount(n, rewards.UnitHours)
}

func date(year int, month time.Month, day int) generic.TimePoint {
	return generic.NewTimePoint(year, month, day)
}

func year2025Period() generic.Period {
	return generic.Period{
		Start: date(2025, time.January, 1),
		End:   date(2025, time.December, 31),
	}
}

// =============================================================================
// WELLNESS POINTS TESTS
// =============================================================================

func TestWellnessPoints_MonthlyAccrual(t *testing.T) {
	// GIVEN: Wellness policy with 1200 points/year (100/month)
	// WHEN: Checking accruals for full year
	// THEN: Should have 12 monthly accruals of 100 points each

	config := rewards.WellnessPointsPolicy("wellness", "Wellness Program", 1200, 200)

	events := config.Accrual.GenerateAccruals(
		date(2025, time.January, 1),
		date(2025, time.December, 31),
	)

	if len(events) != 12 {
		t.Errorf("expected 12 monthly accruals, got %d", len(events))
	}

	for i, e := range events {
		if !e.Amount.Value.Equal(points(100).Value) {
			t.Errorf("event %d: expected 100 points, got %v", i, e.Amount.Value)
		}
	}
}

func TestWellnessPoints_EarnThenSpend(t *testing.T) {
	// GIVEN: Employee earns 100 points in January
	// WHEN: Trying to spend 150 points
	// THEN: Should fail (ConsumeUpToAccrued mode)

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	config := rewards.WellnessPointsPolicy("wellness", "Wellness", 1200, 200)

	// Earn 100 points
	ledger.Append(ctx, generic.Transaction{
		ID:             "tx-earn",
		EntityID:       "emp-1",
		PolicyID:       "wellness",
		ResourceType:   rewards.ResourceWellnessPoints,
		EffectiveAt:    date(2025, time.January, 1),
		Delta:          points(100),
		Type:           generic.TxGrant,
		IdempotencyKey: "earn-jan",
	})

	// Try to validate spending 150 points
	engine := &generic.ProjectionEngine{Ledger: ledger}
	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "wellness",
		Unit:            rewards.UnitPoints,
		Period:          year2025Period(),
		AsOf:            date(2025, time.January, 15),
		Accruals:        config.Accrual,
		RequestedAmount: points(150),
		ConsumptionMode: generic.ConsumeUpToAccrued,
		AllowNegative:   false,
	})

	if result.IsValid {
		t.Error("should NOT allow spending 150 points when only 100 earned")
	}

	// But 50 points should work
	result2, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "wellness",
		Unit:            rewards.UnitPoints,
		Period:          year2025Period(),
		AsOf:            date(2025, time.January, 15),
		Accruals:        config.Accrual,
		RequestedAmount: points(50),
		ConsumptionMode: generic.ConsumeUpToAccrued,
		AllowNegative:   false,
	})

	if !result2.IsValid {
		t.Error("should allow spending 50 points when 100 earned")
	}
}

func TestWellnessPoints_Carryover(t *testing.T) {
	// GIVEN: 300 points remaining at year end, max carryover 200
	// WHEN: Process rollover
	// THEN: 200 carried over, 100 expired

	config := rewards.WellnessPointsPolicy("wellness", "Wellness", 1200, 200)
	engine := &generic.ReconciliationEngine{}

	currentBalance := generic.Balance{
		EntityID:         "emp-1",
		PolicyID:         "wellness",
		Period:           year2025Period(),
		AccruedToDate:    points(1200),
		TotalEntitlement: points(1200),
		TotalConsumed:    points(900), // 300 remaining
	}

	output, err := engine.Process(generic.ReconciliationInput{
		EntityID:       "emp-1",
		PolicyID:       "wellness",
		Policy:         config.Policy,
		CurrentBalance: currentBalance,
		EndingPeriod:   year2025Period(),
		NextPeriod:     generic.Period{Start: date(2026, time.January, 1), End: date(2026, time.December, 31)},
	})

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if !output.Summary.CarriedOver.Value.Equal(points(200).Value) {
		t.Errorf("expected 200 points carried over, got %v", output.Summary.CarriedOver.Value)
	}
	if !output.Summary.Expired.Value.Equal(points(100).Value) {
		t.Errorf("expected 100 points expired, got %v", output.Summary.Expired.Value)
	}
}

// =============================================================================
// LEARNING CREDITS TESTS
// =============================================================================

func TestLearningCredits_FullBudgetAvailableImmediately(t *testing.T) {
	// GIVEN: $2500 learning budget
	// WHEN: Checking balance in January
	// THEN: Full $2500 available (ConsumeAhead mode)

	config := rewards.LearningCreditsPolicy("learning", "L&D Budget", 2500)

	// Should be able to consume full budget immediately
	if config.Policy.ConsumptionMode != generic.ConsumeAhead {
		t.Error("learning credits should be ConsumeAhead")
	}
}

func TestLearningCredits_UseItOrLoseIt(t *testing.T) {
	// GIVEN: $1000 remaining at year end
	// WHEN: Process rollover
	// THEN: All $1000 expires (no carryover)

	config := rewards.LearningCreditsPolicy("learning", "L&D", 2500)
	engine := &generic.ReconciliationEngine{}

	currentBalance := generic.Balance{
		EntityID:         "emp-1",
		PolicyID:         "learning",
		Period:           year2025Period(),
		AccruedToDate:    dollars(2500),
		TotalEntitlement: dollars(2500),
		TotalConsumed:    dollars(1500), // 1000 remaining
	}

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID:       "emp-1",
		PolicyID:       "learning",
		Policy:         config.Policy,
		CurrentBalance: currentBalance,
		EndingPeriod:   year2025Period(),
		NextPeriod:     generic.Period{Start: date(2026, time.January, 1), End: date(2026, time.December, 31)},
	})

	if !output.Summary.Expired.Value.Equal(dollars(1000).Value) {
		t.Errorf("expected $1000 expired, got %v", output.Summary.Expired.Value)
	}
	if !output.Summary.CarriedOver.Value.IsZero() {
		t.Errorf("expected $0 carried over, got %v", output.Summary.CarriedOver.Value)
	}
}

func TestLearningCredits_TrackExpenses(t *testing.T) {
	// GIVEN: $2500 budget
	// WHEN: Spending on course ($399) and conference ($800)
	// THEN: $1301 remaining

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	// Grant budget
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-grant", EntityID: "emp-1", PolicyID: "learning",
		ResourceType: rewards.ResourceLearningCredits,
		EffectiveAt:  date(2025, time.January, 1),
		Delta:        dollars(2500), Type: generic.TxGrant,
		IdempotencyKey: "grant",
	})

	// Course expense
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-course", EntityID: "emp-1", PolicyID: "learning",
		ResourceType: rewards.ResourceLearningCredits,
		EffectiveAt:  date(2025, time.January, 20),
		Delta:        dollars(-399), Type: generic.TxConsumption,
		Reason: "Udemy course bundle", IdempotencyKey: "course",
	})

	// Conference expense
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-conf", EntityID: "emp-1", PolicyID: "learning",
		ResourceType: rewards.ResourceLearningCredits,
		EffectiveAt:  date(2025, time.March, 5),
		Delta:        dollars(-800), Type: generic.TxConsumption,
		Reason: "GopherCon registration", IdempotencyKey: "conf",
	})

	// Check balance
	txs, _ := ledger.TransactionsInRange(ctx, "emp-1", "learning", date(2025, time.January, 1), date(2025, time.December, 31))
	balance := calculateBalance(txs)

	expected := dollars(1301)
	if !balance.Value.Equal(expected.Value) {
		t.Errorf("expected $1301 remaining, got %v", balance.Value)
	}
}

// =============================================================================
// RECOGNITION POINTS TESTS
// =============================================================================

func TestRecognitionPoints_KudosReceived(t *testing.T) {
	// GIVEN: Employee receives kudos from peers
	// WHEN: Summing all kudos
	// THEN: Balance reflects total points received

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	// Receive kudos
	kudos := []struct {
		from   string
		points float64
		reason string
	}{
		{"maria", 50, "Great job on API redesign"},
		{"james", 25, "Helped debug production issue"},
		{"manager", 100, "Q4 performance bonus"},
	}

	for i, k := range kudos {
		ledger.Append(ctx, generic.Transaction{
			ID:             generic.TransactionID(fmt.Sprintf("kudos-%d", i)),
			EntityID:       "emp-1",
			PolicyID:       "recognition",
			ResourceType:   rewards.ResourceRecognitionPoints,
			EffectiveAt:    date(2025, time.January, 15+i),
			Delta:          points(k.points),
			Type:           generic.TxGrant,
			Reason:         fmt.Sprintf("Kudos from @%s: %s", k.from, k.reason),
			IdempotencyKey: fmt.Sprintf("kudos-%d", i),
		})
	}

	txs, _ := ledger.TransactionsInRange(ctx, "emp-1", "recognition", date(2025, time.January, 1), date(2025, time.December, 31))
	balance := calculateBalance(txs)

	expected := points(175) // 50 + 25 + 100
	if !balance.Value.Equal(expected.Value) {
		t.Errorf("expected 175 points, got %v", balance.Value)
	}
}

func TestRecognitionPoints_Redemption(t *testing.T) {
	// GIVEN: Employee has 175 points
	// WHEN: Redeeming 100 points for gift card
	// THEN: 75 points remaining

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	// Earn points
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-earn", EntityID: "emp-1", PolicyID: "recognition",
		ResourceType: rewards.ResourceRecognitionPoints,
		EffectiveAt:  date(2025, time.January, 15),
		Delta:        points(175), Type: generic.TxGrant,
		IdempotencyKey: "earn",
	})

	// Redeem
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-redeem", EntityID: "emp-1", PolicyID: "recognition",
		ResourceType: rewards.ResourceRecognitionPoints,
		EffectiveAt:  date(2025, time.February, 1),
		Delta:        points(-100), Type: generic.TxConsumption,
		Reason: "Redeemed for $50 Amazon gift card", IdempotencyKey: "redeem",
	})

	txs, _ := ledger.TransactionsInRange(ctx, "emp-1", "recognition", date(2025, time.January, 1), date(2025, time.December, 31))
	balance := calculateBalance(txs)

	expected := points(75)
	if !balance.Value.Equal(expected.Value) {
		t.Errorf("expected 75 points remaining, got %v", balance.Value)
	}
}

// =============================================================================
// FLEX BENEFITS TESTS
// =============================================================================

func TestFlexBenefits_WithCarryover(t *testing.T) {
	// GIVEN: $1500 budget + $350 carryover from last year
	// WHEN: Checking balance
	// THEN: $1850 total available

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	// Annual grant
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-grant", EntityID: "emp-1", PolicyID: "flex",
		ResourceType: rewards.ResourceFlexBenefits,
		EffectiveAt:  date(2025, time.January, 1),
		Delta:        dollars(1500), Type: generic.TxGrant,
		IdempotencyKey: "grant",
	})

	// Carryover
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-carryover", EntityID: "emp-1", PolicyID: "flex",
		ResourceType: rewards.ResourceFlexBenefits,
		EffectiveAt:  date(2025, time.January, 1),
		Delta:        dollars(350), Type: generic.TxReconciliation,
		Reason: "Carryover from 2024", IdempotencyKey: "carryover",
	})

	txs, _ := ledger.TransactionsInRange(ctx, "emp-1", "flex", date(2025, time.January, 1), date(2025, time.December, 31))
	balance := calculateBalance(txs)

	expected := dollars(1850)
	if !balance.Value.Equal(expected.Value) {
		t.Errorf("expected $1850, got %v", balance.Value)
	}
}

// =============================================================================
// REMOTE WORK DAYS TESTS
// =============================================================================

func TestRemoteWorkDays_MonthlyAllowance(t *testing.T) {
	// GIVEN: 8 WFH days per month
	// WHEN: Checking accruals for Q1
	// THEN: Should have 24 days (3 months × 8)

	config := rewards.RemoteWorkDaysPolicy("wfh", "Remote Work", 8)

	events := config.Accrual.GenerateAccruals(
		date(2025, time.January, 1),
		date(2025, time.March, 31),
	)

	if len(events) != 3 {
		t.Errorf("expected 3 monthly accruals, got %d", len(events))
	}

	total := generic.NewAmount(0, rewards.UnitDays)
	for _, e := range events {
		total = total.Add(e.Amount)
	}

	expected := generic.NewAmount(24, rewards.UnitDays)
	if !total.Value.Equal(expected.Value) {
		t.Errorf("expected 24 days, got %v", total.Value)
	}
}

// =============================================================================
// VOLUNTEER HOURS TESTS
// =============================================================================

func TestVolunteerHours_UpfrontGrant(t *testing.T) {
	// GIVEN: 16 volunteer hours per year
	// WHEN: Checking accruals
	// THEN: Single upfront grant of 16 hours

	config := rewards.VolunteerHoursPolicy("volunteer", "Volunteer Time", 16)

	events := config.Accrual.GenerateAccruals(
		date(2025, time.January, 1),
		date(2025, time.December, 31),
	)

	if len(events) != 1 {
		t.Errorf("expected 1 upfront accrual, got %d", len(events))
	}

	if !events[0].Amount.Value.Equal(hours(16).Value) {
		t.Errorf("expected 16 hours, got %v", events[0].Amount.Value)
	}
}

func TestVolunteerHours_Usage(t *testing.T) {
	// GIVEN: 16 hours granted
	// WHEN: Using 4 hours for food bank
	// THEN: 12 hours remaining

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	ledger.Append(ctx, generic.Transaction{
		ID: "tx-grant", EntityID: "emp-1", PolicyID: "volunteer",
		ResourceType: rewards.ResourceVolunteerHours,
		EffectiveAt:  date(2025, time.January, 1),
		Delta:        hours(16), Type: generic.TxGrant,
		IdempotencyKey: "grant",
	})

	ledger.Append(ctx, generic.Transaction{
		ID: "tx-use", EntityID: "emp-1", PolicyID: "volunteer",
		ResourceType: rewards.ResourceVolunteerHours,
		EffectiveAt:  date(2025, time.February, 14),
		Delta:        hours(-4), Type: generic.TxConsumption,
		Reason: "Food bank volunteering", IdempotencyKey: "foodbank",
	})

	txs, _ := ledger.TransactionsInRange(ctx, "emp-1", "volunteer", date(2025, time.January, 1), date(2025, time.December, 31))
	balance := calculateBalance(txs)

	expected := hours(12)
	if !balance.Value.Equal(expected.Value) {
		t.Errorf("expected 12 hours remaining, got %v", balance.Value)
	}
}

// =============================================================================
// MULTI-RESOURCE TYPE TESTS
// =============================================================================

func TestMultipleRewardTypes_IndependentBalances(t *testing.T) {
	// GIVEN: Employee with wellness points + learning credits + recognition points
	// WHEN: Consuming from each independently
	// THEN: Balances are separate and don't affect each other

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	// Grant different resource types
	resources := []struct {
		policyID string
		resType  generic.ResourceType
		amount   generic.Amount
	}{
		{"wellness", rewards.ResourceWellnessPoints, points(1000)},
		{"learning", rewards.ResourceLearningCredits, dollars(2500)},
		{"recognition", rewards.ResourceRecognitionPoints, points(175)},
	}

	for _, r := range resources {
		ledger.Append(ctx, generic.Transaction{
			ID:             generic.TransactionID("grant-" + string(r.policyID)),
			EntityID:       "emp-1",
			PolicyID:       generic.PolicyID(r.policyID),
			ResourceType:   r.resType,
			EffectiveAt:    date(2025, time.January, 1),
			Delta:          r.amount,
			Type:           generic.TxGrant,
			IdempotencyKey: "grant-" + string(r.policyID),
		})
	}

	// Consume from wellness only
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-wellness-use", EntityID: "emp-1", PolicyID: "wellness",
		ResourceType: rewards.ResourceWellnessPoints,
		EffectiveAt:  date(2025, time.February, 1),
		Delta:        points(-500), Type: generic.TxConsumption,
		IdempotencyKey: "wellness-use",
	})

	// Check wellness balance (should be 500)
	wellnessTxs, _ := ledger.TransactionsInRange(ctx, "emp-1", "wellness", date(2025, time.January, 1), date(2025, time.December, 31))
	wellnessBalance := calculateBalance(wellnessTxs)
	if !wellnessBalance.Value.Equal(points(500).Value) {
		t.Errorf("wellness: expected 500 points, got %v", wellnessBalance.Value)
	}

	// Check learning balance (should be unchanged at 2500)
	learningTxs, _ := ledger.TransactionsInRange(ctx, "emp-1", "learning", date(2025, time.January, 1), date(2025, time.December, 31))
	learningBalance := calculateBalance(learningTxs)
	if !learningBalance.Value.Equal(dollars(2500).Value) {
		t.Errorf("learning: expected $2500, got %v", learningBalance.Value)
	}

	// Check recognition balance (should be unchanged at 175)
	recognitionTxs, _ := ledger.TransactionsInRange(ctx, "emp-1", "recognition", date(2025, time.January, 1), date(2025, time.December, 31))
	recognitionBalance := calculateBalance(recognitionTxs)
	if !recognitionBalance.Value.Equal(points(175).Value) {
		t.Errorf("recognition: expected 175 points, got %v", recognitionBalance.Value)
	}
}

// =============================================================================
// HELPER
// =============================================================================

func calculateBalance(txs []generic.Transaction) generic.Amount {
	if len(txs) == 0 {
		return generic.Amount{}
	}
	total := txs[0].Delta.Zero()
	for _, tx := range txs {
		total = total.Add(tx.Delta)
	}
	return total
}
