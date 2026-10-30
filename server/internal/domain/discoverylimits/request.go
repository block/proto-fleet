package discoverylimits

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	// MaxReportsPerRequest bounds raw discovery reports across commands and nodes.
	MaxReportsPerRequest = 262144
	// MaxUniqueDevicesPerRequest bounds streamed results and their deduplication map.
	MaxUniqueDevicesPerRequest = 65536

	partialResultsAdvice  = "results are partial. Narrow the targets and continue discovery manually."
	RequestTimeoutWarning = "discovery request timed out; " + partialResultsAdvice
)

var (
	ReportLimitWarning = fmt.Sprintf("discovery stopped at the request report limit (%d reports); %s", MaxReportsPerRequest, partialResultsAdvice)
	ResultLimitWarning = fmt.Sprintf("discovery stopped at the request result limit (%d unique devices); %s", MaxUniqueDevicesPerRequest, partialResultsAdvice)
)

// ReportBudget is shared by every discovery command dispatched for a request.
// Only accepted reports are charged, when published; charges are not returned.
// Rejecting a batch exhausts the request even if a smaller batch could still fit.
type ReportBudget struct {
	mu        sync.Mutex
	reported  int
	exhausted bool
	deadline  time.Time
}

type requestBudgetKey struct{}

// WithRequest bounds the whole request, retaining any outer budget and deadline.
// Direct-node callers get the same protection as the public discovery fan-out.
func WithRequest(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if BudgetFromContext(ctx) == nil {
		ctx = context.WithValue(ctx, requestBudgetKey{}, &ReportBudget{deadline: time.Now().Add(timeout)})
	}
	return context.WithTimeout(ctx, timeout)
}

func BudgetFromContext(ctx context.Context) *ReportBudget {
	budget, _ := ctx.Value(requestBudgetKey{}).(*ReportBudget)
	return budget
}

// Admit reserves a complete batch. A false result permanently closes admission.
func (b *ReportBudget) Admit(count int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exhausted || count < 0 || count > MaxReportsPerRequest-b.reported {
		b.exhausted = true
		return false
	}
	b.reported += count
	b.exhausted = b.reported == MaxReportsPerRequest
	return true
}

// DeadlineExpired reports whether the request's aggregate deadline has passed,
// as opposed to an earlier deadline or cancellation from the caller.
func (b *ReportBudget) DeadlineExpired() bool {
	return !time.Now().Before(b.deadline)
}

func (b *ReportBudget) Exhausted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exhausted
}
