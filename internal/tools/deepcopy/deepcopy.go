package deepcopy

import (
	"fmt"
	"math"
)

// Note: forked from plumbing/maps/deepcopy.go
// modified the float handling
func DeepCopyJSONValue(x any) any {
	switch x := x.(type) {
	case map[string]any:
		if x == nil {
			// Typed nil - an any that contains a type map[string]any with a value of nil
			return x
		}
		clone := make(map[string]any, len(x))
		for k, v := range x {
			clone[k] = DeepCopyJSONValue(v)
		}
		return clone
	case []any:
		if x == nil {
			// Typed nil - an any that contains a type []any with a value of nil
			return x
		}
		clone := make([]any, len(x))
		for i, v := range x {
			clone[i] = DeepCopyJSONValue(v)
		}
		return clone
	case []map[string]any:
		if x == nil {
			return x
		}
		clone := make([]any, len(x))
		for i, v := range x {
			clone[i] = DeepCopyJSONValue(v)
		}
		return clone
	case string, int64, bool, nil:
		return x
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case float32:
		if x >= math.MinInt64 && x <= math.MaxInt64 {
			return int64(x)
		}
	case float64:
		if x >= math.MinInt64 && x <= math.MaxInt64 {
			return int64(x)
		}
	default:
		return fmt.Sprintf("%v", x)
	}
	return fmt.Sprintf("%v", x) // Fallback for unsupported types
}
