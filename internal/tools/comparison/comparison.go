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

		if reflect.TypeOf(value).Kind() != reflect.TypeOf(rmValue).Kind() {
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
				// fmt.Printf("Values are not both slices or type assertion failed at '%s'\n", pathStr)
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "values are not both slices or type assertion failed",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, fmt.Errorf("values are not both slices or type assertion failed at %s", pathStr)
			}
			rmSlice, ok2 := rmValue.([]interface{})
			if !ok2 {
				// fmt.Printf("Type assertion failed for slice at '%s'\n", pathStr)
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "values are not both slices or type assertion failed",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, fmt.Errorf("type assertion failed for slice at %s", pathStr)
			}
			for i, v := range valueSlice {
				if reflect.TypeOf(v).Kind() == reflect.Map {
					mgMap, ok1 := v.(map[string]interface{})
					if !ok1 {
						// fmt.Printf("Type assertion failed for map at '%s'\n", pathStr)
						return ComparisonResult{
							IsEqual: false,
							Reason: &Reason{
								Reason:      "type assertion failed",
								FirstValue:  value,
								SecondValue: rmValue,
							},
						}, fmt.Errorf("type assertion failed for map at %s", pathStr)
					}
					rmMap, ok2 := rmSlice[i].(map[string]interface{})
					if !ok2 {
						// fmt.Printf("Type assertion failed for map at '%s'\n", pathStr)
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
						// fmt.Printf("Values differ at '%s'\n", pathStr)
						return ComparisonResult{
							IsEqual: false,
							Reason: &Reason{
								Reason:      "values differ",
								FirstValue:  value,
								SecondValue: rmValue,
							},
						}, nil
					}
				} else if v != rmSlice[i] {
					// fmt.Printf("Values differ at '%s'\n", pathStr)
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
