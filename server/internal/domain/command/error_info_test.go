package command

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
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
