package comparison

import (
	"fmt"
	"reflect"
)

type Reason struct {
	Reason      string
	FirstValue  any
	SecondValue any
}

type ComparisonResult struct {
	IsEqual bool
	Reason  *Reason
}

func (r ComparisonResult) String() string {
	if r.IsEqual {
		return "ComparisonResult: IsEqual=true"
	}
	if r.Reason == nil {
		return "ComparisonResult: IsEqual=false, Reason=nil"
	}
	return fmt.Sprintf("ComparisonResult: IsEqual=false, Reason=%s, FirstValue=%v, SecondValue=%v", r.Reason.Reason, r.Reason.FirstValue, r.Reason.SecondValue)
}

// compareExisting recursively compares fields between two maps and logs differences.
func CompareExisting(mg map[string]interface{}, rm map[string]interface{}, path ...string) (ComparisonResult, error) {
	for key, value := range mg {
		currentPath := append(path, key)
		pathStr := fmt.Sprintf("%v", currentPath)

		rmValue, ok := rm[key]
		if !ok {
			continue
		}

		// Handle case where one or both values are nil
		if value == nil || rmValue == nil {
			if value == nil && rmValue == nil {
				continue // Both are nil, considered equal
			}
			// One is nil but the other isn't, so they are not equal.
			return ComparisonResult{
				IsEqual: false,
				Reason: &Reason{
					Reason:      "values differ (one is nil)",
					FirstValue:  value,
					SecondValue: rmValue,
				},
			}, nil
		}

		if !isNumeric(reflect.TypeOf(value).Kind()) && !isNumeric(reflect.TypeOf(rmValue).Kind()) && reflect.TypeOf(value).Kind() != reflect.TypeOf(rmValue).Kind() {
			return ComparisonResult{
				IsEqual: false,
				Reason: &Reason{
					Reason:      "types differ",
					FirstValue:  value,
					SecondValue: rmValue,
				},
			}, fmt.Errorf("types differ at %s - %s is different from %s", pathStr, reflect.TypeOf(value).Kind(), reflect.TypeOf(rmValue).Kind())
		}

		switch reflect.TypeOf(value).Kind() {
		case reflect.Map:
			mgMap, ok1 := value.(map[string]interface{})
			if !ok1 {
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "type assertion failed",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, fmt.Errorf("type assertion failed for map at %s", pathStr)
			}
			rmMap, ok2 := rmValue.(map[string]interface{})
			if !ok2 {
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "type assertion failed",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, fmt.Errorf("type assertion failed for map at %s", pathStr)
			}
			res, err := CompareExisting(mgMap, rmMap, currentPath...)
			if err != nil {
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "error comparing maps",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, err
			}
			if !res.IsEqual {
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "values differ",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, nil
			}
		case reflect.Slice:
			valueSlice, ok1 := value.([]interface{})
			if !ok1 || reflect.TypeOf(rmValue).Kind() != reflect.Slice {
				return ComparisonResult{IsEqual: false, Reason: &Reason{Reason: "values are not both slices or type assertion failed"}}, fmt.Errorf("values are not both slices or type assertion failed at %s", pathStr)
			}
			rmSlice, ok2 := rmValue.([]interface{})
			if !ok2 {
				return ComparisonResult{IsEqual: false, Reason: &Reason{Reason: "type assertion failed for remote slice"}}, fmt.Errorf("type assertion failed for remote slice at %s", pathStr)
			}

			if len(valueSlice) != len(rmSlice) {
				return ComparisonResult{IsEqual: false, Reason: &Reason{Reason: "slice lengths are different"}}, nil
			}

			// If the slice is empty, they are equal.
			if len(valueSlice) == 0 {
				break
			}

			// Determine if we are comparing slices of maps or slices of primitives.
			if _, ok := valueSlice[0].(map[string]interface{}); ok {
				// Handling for slice of maps (set comparison)
				matched := make([]bool, len(rmSlice))
				for _, v := range valueSlice {
					mgMap, ok := v.(map[string]interface{})
					if !ok {
						return ComparisonResult{IsEqual: false, Reason: &Reason{Reason: "local slice item is not a map, but expected to be"}}, nil
					}

					foundMatch := false
					for i, r := range rmSlice {
						if matched[i] {
							continue
						}
						rmMap, ok := r.(map[string]interface{})
						if !ok {
							continue
						}

						// We need recursive comparison for maps
						res, err := CompareExisting(mgMap, rmMap, currentPath...)
						if err == nil && res.IsEqual {
							matched[i] = true
							foundMatch = true
							break
						}
					}

					if !foundMatch {
						return ComparisonResult{IsEqual: false, Reason: &Reason{Reason: "item not found in remote slice"}}, nil
					}
				}
			} else {
				// Handling for slice of primitives
				// We build a count map for both slices to compare their elements.
				// This allows us to handle cases where the order of elements may differ.
				mgCounts := make(map[interface{}]int)
				for _, item := range valueSlice {
					mgCounts[item]++
				}

				rmCounts := make(map[interface{}]int)
				for _, item := range rmSlice {
					rmCounts[item]++
				}

				if !reflect.DeepEqual(mgCounts, rmCounts) {
					return ComparisonResult{IsEqual: false, Reason: &Reason{Reason: "primitive slices have different elements"}}, nil
				}
			}
		default:
			ok, err := compareAny(value, rmValue)
			if err != nil {
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "error comparing values",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, err
			}
			if !ok {
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "values differ",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, nil
			}
		}
	}

	return ComparisonResult{IsEqual: true}, nil
}
func isNumeric(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func numberCaster(value interface{}) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v // No conversion needed since v is already int64
	case uint:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	case float32:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return -999999 // Return a default value if none of the cases match
	}
}

func compareAny(a any, b any) (bool, error) {
	//if is number compare as number
	switch a.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		ia := numberCaster(a)
		ib := numberCaster(b)
		return ia == ib, nil
	case string:
		sa, ok := a.(string)
		if !ok {
			return false, fmt.Errorf("type assertion failed - to string: %v", a)
		}
		sb, ok := b.(string)
		if !ok {
			return false, fmt.Errorf("type assertion failed - to string: %v", b)
		}
		return sa == sb, nil
	case bool:
		ba, ok := a.(bool)
		if !ok {
			return false, fmt.Errorf("type assertion failed - to bool: %v", a)
		}
		bb, ok := b.(bool)
		if !ok {
			return false, fmt.Errorf("type assertion failed - to bool: %v", b)
		}
		return ba == bb, nil
	default:
		return reflect.DeepEqual(a, b), nil
	}
}
