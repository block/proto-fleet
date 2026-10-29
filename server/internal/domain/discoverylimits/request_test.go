package discoverylimits

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWithRequestRetainsBudgetAndDeadline(t *testing.T) {
	outer, cancel := WithRequest(t.Context(), time.Second)
	defer cancel()
	inner, cancelInner := WithRequest(outer, time.Minute)
	defer cancelInner()
	require.Same(t, BudgetFromContext(outer), BudgetFromContext(inner))
	outerDeadline, ok := outer.Deadline()
	require.True(t, ok)
	innerDeadline, ok := inner.Deadline()
	require.True(t, ok)
	require.Equal(t, outerDeadline, innerDeadline)

	independent, cancelIndependent := WithRequest(t.Context(), time.Minute)
	defer cancelIndependent()
	require.NotSame(t, BudgetFromContext(outer), BudgetFromContext(independent))
}

func TestReportBudgetRejectsCrossingBatchAndClosesAdmission(t *testing.T) {
	budget := &ReportBudget{}
	require.True(t, budget.Admit(MaxReportsPerRequest-1))
	require.False(t, budget.Admit(2))
	require.Equal(t, MaxReportsPerRequest-1, budget.reported, "rejected batch must not reserve or persist a prefix")
	require.True(t, budget.Exhausted())
	require.False(t, budget.Admit(1), "a rejected batch stops the whole request")
}

func TestReportBudgetConcurrentAdmissionCannotExceedLimit(t *testing.T) {
	budget := &ReportBudget{}
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for budget.Admit(1024) {
				admitted.Add(1024)
			}
		})
	}
	wg.Wait()
	require.Equal(t, int64(MaxReportsPerRequest), admitted.Load())
	require.True(t, budget.Exhausted())
}
