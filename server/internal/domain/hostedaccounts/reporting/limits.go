package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrOutputLimit identifies a per-artifact byte budget failure.
var ErrOutputLimit = errors.New("reporting output exceeds supported byte limit")

// These are per-operation work and memory budgets, not account or fleet quotas.
const (
	MaxReportBreakdowns     = 1024
	MaxResultJSONBytes      = 8 << 20
	MaxWrittenReportBytes   = 16 << 20
	MaxEvidenceZIPBytes     = 64 << 20
	MaxEvidenceContentBytes = 256 << 20
)

type outputWriter struct {
	checkCtx  func() error
	writer    io.Writer
	remaining int64
	bounded   bool
}

func (w *outputWriter) Write(p []byte) (int, error) {
	if err := w.checkCtx(); err != nil {
		return 0, fmt.Errorf("write reporting output: %w", err)
	}
	if w.bounded && int64(len(p)) > w.remaining {
		return 0, ErrOutputLimit
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	if err != nil {
		return n, fmt.Errorf("write reporting output: %w", err)
	}
	return n, nil
}

func resultLimits(ctx context.Context, result Result) error {
	if len(result.Containers)+len(result.Orders) > MaxReportBreakdowns {
		return fmt.Errorf("report exceeds %d supported breakdowns", MaxReportBreakdowns)
	}
	remaining := int64(MaxResultJSONBytes)
	return jsonBudget(ctx, reflect.ValueOf(result), &remaining)
}

// MarshalResultContext checks the complete encoded-size upper bound before json
// allocates its encoding buffer. Large repeated source lists cannot expand an
// accepted input into an unbounded result serialization.
func MarshalResultContext(ctx context.Context, result Result) ([]byte, error) {
	if err := resultLimits(ctx, result); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode report result: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("encode report result: %w", err)
	}
	if len(encoded) > MaxResultJSONBytes {
		return nil, fmt.Errorf("report result exceeds supported byte limit")
	}
	return encoded, nil
}

// jsonBudget visits the report's JSON fields without materializing JSON. It
// includes omitted fields and a trailing comma per member, so it is an upper
// bound; numbers and time values have small fixed upper bounds.
func jsonBudget(ctx context.Context, v reflect.Value, remaining *int64) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("bound report result: %w", err)
	}
	spend := func(n int64) error {
		*remaining -= n
		if *remaining < 0 {
			return fmt.Errorf("report result exceeds supported byte limit")
		}
		return nil
	}
	if !v.IsValid() {
		return spend(4)
	}
	if v.Type() == reflect.TypeOf(time.Time{}) {
		return spend(64)
	}
	switch v.Kind() {
	case reflect.String:
		value := v.String()
		if int64(len(value))+2 > *remaining {
			return fmt.Errorf("report result exceeds supported byte limit")
		}
		if err := spend(2); err != nil {
			return err
		}
		nextCheck := 0
		for offset := 0; offset < len(value); {
			if offset >= nextCheck {
				nextCheck = offset + 4096
				if err := ctx.Err(); err != nil {
					return fmt.Errorf("bound report result: %w", err)
				}
			}
			r, n := utf8.DecodeRuneInString(value[offset:])
			size := n
			switch {
			case r == '"' || r == '\\' || r == '\b' || r == '\f' || r == '\n' || r == '\r' || r == '\t':
				size = 2
			case r < 0x20 || r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029' || (r == utf8.RuneError && n == 1):
				size = 6
			}
			if err := spend(int64(size)); err != nil {
				return err
			}
			offset += n
		}
		return nil
	case reflect.Bool:
		return spend(5)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return spend(32)
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return spend(4)
		}
		return jsonBudget(ctx, v.Elem(), remaining)
	case reflect.Struct:
		if err := spend(2); err != nil {
			return err
		}
		for i := range v.NumField() {
			field := v.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			if err := jsonBudget(ctx, reflect.ValueOf(name), remaining); err != nil {
				return err
			}
			if err := spend(2); err != nil {
				return err
			}
			if err := jsonBudget(ctx, v.Field(i), remaining); err != nil {
				return err
			}
		}
		return nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return spend(4)
		}
		if err := spend(2); err != nil {
			return err
		}
		for i := range v.Len() {
			if err := spend(1); err != nil {
				return err
			}
			if err := jsonBudget(ctx, v.Index(i), remaining); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if v.IsNil() {
			return spend(4)
		}
		if err := spend(2); err != nil {
			return err
		}
		iterator := v.MapRange()
		for iterator.Next() {
			if err := spend(2); err != nil {
				return err
			}
			if err := jsonBudget(ctx, iterator.Key(), remaining); err != nil {
				return err
			}
			if err := jsonBudget(ctx, iterator.Value(), remaining); err != nil {
				return err
			}
		}
		return nil
	case reflect.Invalid, reflect.Uintptr, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return fmt.Errorf("unsupported report result field")
	}
	return fmt.Errorf("unsupported report result field")
}
