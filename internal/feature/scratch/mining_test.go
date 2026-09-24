package scratch

import "testing"

func TestMineUsesScratchBudget(t *testing.T) {
	calls := 0
	h := &Handler{mine: true, run: func(attempts uint64) (Result, error) {
		calls++
		if attempts != 10_000 {
			t.Fatalf("attempts = %d, want 10000", attempts)
		}
		return Result{Attempts: attempts, Score: 87}, nil
	}}
	for i := 0; i < 3; i++ {
		result, err := h.Mine()
		if err != nil || result.Attempts != 10_000 || result.Score != 87 {
			t.Fatalf("result = %+v, err = %v", result, err)
		}
	}
	if calls != 3 {
		t.Fatalf("calls = %d", calls)
	}
	h.mine = false
	if _, err := h.Mine(); err == nil || calls != 3 {
		t.Fatal("disabled mining should fail without running")
	}
}
