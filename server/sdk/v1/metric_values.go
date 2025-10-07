package sdk

type allowedValueTypes interface {
	int | int64 | float64 | string | bool
}

func NewMetricValue[T allowedValueTypes](value T) MetricValue {
	switch v := any(value).(type) {
	case int:
		return IntValue{Value: v}
	case int64:
		return IntValue{Value: int(v)}
	case float64:
		return Float64Value{Value: v}
	case string:
		return StringValue{Value: v}
	case bool:
		return BoolValue{Value: v}
	default:
		return nil
	}
}

type ValueType int

// ValueType constants define the different types of metric values supported.
const (
	ValueTypeInt ValueType = iota
	ValueTypeFloat64
	ValueTypeString
	ValueTypeBool
)

type MetricValue interface {
	Type() ValueType
	AsInt() (int, bool)
	AsFloat64() (float64, bool)
	AsString() (string, bool)
	AsBool() (bool, bool)
}

type IntValue struct {
	Value int
}

func (v IntValue) Type() ValueType {
	return ValueTypeInt
}
func (v IntValue) AsInt() (int, bool) {
	return v.Value, true
}
func (v IntValue) AsFloat64() (float64, bool) {
	return float64(v.Value), false
}
func (v IntValue) AsString() (string, bool) {
	return "", false
}
func (v IntValue) AsBool() (bool, bool) {
	return false, false
}

type Float64Value struct {
	Value float64
}

func (v Float64Value) Type() ValueType {
	return ValueTypeFloat64
}
func (v Float64Value) AsInt() (int, bool) {
	return 0, false
}
func (v Float64Value) AsFloat64() (float64, bool) {
	return v.Value, true
}
func (v Float64Value) AsString() (string, bool) {
	return "", false
}
func (v Float64Value) AsBool() (bool, bool) {
	return false, false
}

type StringValue struct {
	Value string
}

func (v StringValue) Type() ValueType {
	return ValueTypeString
}
func (v StringValue) AsInt() (int, bool) {
	return 0, false
}
func (v StringValue) AsFloat64() (float64, bool) {
	return 0, false
}
func (v StringValue) AsString() (string, bool) {
	return v.Value, true
}
func (v StringValue) AsBool() (bool, bool) {
	return false, false
}

type BoolValue struct {
	Value bool
}

func (v BoolValue) Type() ValueType {
	return ValueTypeBool
}
func (v BoolValue) AsInt() (int, bool) {
	return 0, false
}
func (v BoolValue) AsFloat64() (float64, bool) {
	return 0, false
}
func (v BoolValue) AsString() (string, bool) {
	return "", false
}
func (v BoolValue) AsBool() (bool, bool) {
	return v.Value, true
}
