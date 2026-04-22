package command

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestBoundedErrorInfo_EmptyReturnsNull(t *testing.T) {
	got := boundedErrorInfo("")
	if got.Valid {
		t.Fatalf("expected NULL for empty input, got %+v", got)
	}
}

func TestBoundedErrorInfo_ShortPassesThrough(t *testing.T) {
	in := "boom"
	got := boundedErrorInfo(in)
	if !got.Valid {
		t.Fatalf("expected valid NullString")
	}
	if got.String != in {
		t.Fatalf("expected %q, got %q", in, got.String)
	}
}

func TestBoundedErrorInfo_TruncatesAtRuneLimit(t *testing.T) {
	in := strings.Repeat("x", maxErrorInfoRunes*2)
	got := boundedErrorInfo(in)
	if !got.Valid {
		t.Fatalf("expected valid NullString")
	}
	if utf8.RuneCountInString(got.String) != maxErrorInfoRunes {
		t.Fatalf("expected %d runes after truncation, got %d", maxErrorInfoRunes, utf8.RuneCountInString(got.String))
	}
	if !strings.HasSuffix(got.String, truncationSuffix) {
		t.Fatalf("expected suffix %q, got %q", truncationSuffix, got.String)
	}
}

func TestBoundedErrorInfo_HandlesMultibyteRunes(t *testing.T) {
	in := strings.Repeat("🪙", maxErrorInfoRunes*2)
	got := boundedErrorInfo(in)
	if !got.Valid {
		t.Fatalf("expected valid NullString")
	}
	if utf8.RuneCountInString(got.String) > maxErrorInfoRunes {
		t.Fatalf("truncated output exceeded %d runes: %d", maxErrorInfoRunes, utf8.RuneCountInString(got.String))
	}
	if !utf8.ValidString(got.String) {
		t.Fatalf("truncation produced invalid UTF-8: %q", got.String)
	}
}

func TestWorkerErrorInfo_NilErrorReturnsNull(t *testing.T) {
	got := workerErrorInfo(nil)
	if got.Valid {
		t.Fatalf("expected NULL for nil error, got %+v", got)
	}
}

func TestWorkerErrorInfo_PropagatesMessage(t *testing.T) {
	got := workerErrorInfo(errors.New("plugin exploded"))
	if !got.Valid {
		t.Fatalf("expected valid NullString")
	}
	if got.String != "plugin exploded" {
		t.Fatalf("unexpected message: %q", got.String)
	}
}

// sanitizedErrorInfo is the operator-safe path used on the worker
// completion write. These cases cover the four inputs a reviewer worried
// about (nil, bare error, FleetError, wrapped FleetError) plus the
// long-message truncation invariant.

func TestSanitizedErrorInfo_NilReturnsNull(t *testing.T) {
	got := sanitizedErrorInfo(nil)
	if got.Valid {
		t.Fatalf("expected NULL for nil error, got %+v", got)
	}
}

func TestSanitizedErrorInfo_NonFleetErrorCollapsesToGeneric(t *testing.T) {
	// Raw bytes that would leak credentials / file paths / hostnames if
	// echoed verbatim. The sanitizer must replace them with the neutral
	// marker regardless of content.
	attackerControlled := "rootfs:/etc/proto-fleet/secret token=abcd1234 host=bitcoin-wallet.internal"
	got := sanitizedErrorInfo(errors.New(attackerControlled))
	if !got.Valid {
		t.Fatalf("expected valid NullString for non-nil error")
	}
	if got.String != genericWorkerErrorMessage {
		t.Fatalf("non-FleetError must collapse to %q, got %q",
			genericWorkerErrorMessage, got.String)
	}
	if strings.Contains(got.String, "secret") || strings.Contains(got.String, "token=") {
		t.Fatalf("sanitized output leaked raw input: %q", got.String)
	}
}

func TestSanitizedErrorInfo_FleetErrorPassesThrough(t *testing.T) {
	fe := fleeterror.NewInternalErrorf("reading counts for %s: %v", "batch-xyz", "transient")
	got := sanitizedErrorInfo(fe)
	if !got.Valid {
		t.Fatalf("expected valid NullString for FleetError")
	}
	// Format: "<GRPCCode>: <DebugMessage>". GRPCCode stringifies to lower
	// case ("internal").
	if !strings.HasPrefix(got.String, "internal: ") {
		t.Fatalf("expected %q prefix, got %q", "internal: ", got.String)
	}
	if !strings.Contains(got.String, "reading counts for batch-xyz") {
		t.Fatalf("expected DebugMessage to pass through, got %q", got.String)
	}
}

func TestSanitizedErrorInfo_WrappedFleetErrorUnwraps(t *testing.T) {
	// fmt.Errorf with %w preserves the sentinel through errors.As. The
	// sanitizer must surface the wrapped FleetError's code + message, not
	// the outer wrapper's string.
	inner := fleeterror.NewInvalidArgumentError("batch_identifier is required")
	wrapped := fmt.Errorf("handler validation: %w", inner)
	got := sanitizedErrorInfo(wrapped)
	if !got.Valid {
		t.Fatalf("expected valid NullString")
	}
	if !strings.HasPrefix(got.String, "invalid_argument: ") {
		t.Fatalf("expected invalid_argument prefix, got %q", got.String)
	}
}

func TestSanitizedErrorInfo_TruncatesLongFleetErrorMessage(t *testing.T) {
	longMsg := strings.Repeat("x", maxErrorInfoRunes*2)
	fe := fleeterror.NewInternalErrorf("%s", longMsg)
	got := sanitizedErrorInfo(fe)
	if !got.Valid {
		t.Fatalf("expected valid NullString")
	}
	if utf8.RuneCountInString(got.String) > maxErrorInfoRunes {
		t.Fatalf("expected at most %d runes, got %d", maxErrorInfoRunes, utf8.RuneCountInString(got.String))
	}
	if !strings.HasSuffix(got.String, truncationSuffix) {
		t.Fatalf("expected %q suffix on truncation, got %q", truncationSuffix, got.String)
	}
}
