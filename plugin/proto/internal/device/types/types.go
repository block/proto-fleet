package types

// Integer is a constraint that permits any integer type.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// HumanReadableIndex converts a 0-based hardware index to 1-based for human-readable display.
// Hardware components (fans, hashboards, PSUs, ASICs) are 0-indexed in the API but should
// be displayed as 1-indexed to users (e.g., "Fan 1" instead of "Fan 0").
func HumanReadableIndex[T Integer](index T) T {
	return index + 1
}
