package comparison

import (
	"fmt"
	"reflect"

	"github.com/google/go-cmp/cmp"
	"github.com/krateoplatformops/plumbing/jqutil"
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

// CompareExisting recursively compares fields between two maps and logs differences.
// If a field exists in the first map but not in the second, it is ignored.
// If a field exists in the second map but not in the first, it is ignored.
// If both maps have the same field, it compares their values.
// Slices order is considered, so if the order of elements in slices is different, they are considered unequal.
// If the values are maps or slices, it recursively compares them.
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
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "values are not both slices or type assertion failed",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, fmt.Errorf("type assertion failed for slice at %s", pathStr)
			}
			// If the first slice is longer than the second, they are not equal.
			// If the second slice is longer than the first, we ignore it because we are only comparing fields that exist in the first map.
			if len(valueSlice) > len(rmSlice) {
				return ComparisonResult{
					IsEqual: false,
					Reason: &Reason{
						Reason:      "first slice is longer than second",
						FirstValue:  value,
						SecondValue: rmValue,
					},
				}, nil
			}

			for i, v := range valueSlice {
				if reflect.TypeOf(v).Kind() == reflect.Map {
					mgMap, ok1 := v.(map[string]interface{})
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
					rmMap, ok2 := rmSlice[i].(map[string]interface{})
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
				} else if v != rmSlice[i] {
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
			ok := compareAny(value, rmValue)
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

func compareAny(a any, b any) bool {
	strA := fmt.Sprintf("%v", a)
	strB := fmt.Sprintf("%v", b)

	a = jqutil.InferType(strA)
	b = jqutil.InferType(strB)

	return cmp.Equal(a, b)
}

// DeepEqual performs a deep comparison between two values.
// It is suitable for comparing also complex structures like maps and slices.
// For maps (objects), key order does not matter.
// For slices (arrays), element order and content are strictly compared.
func DeepEqual(a, b interface{}) bool {
	// PROBABLY NOT NEEDED
	// For complex types, a direct recursive comparison is correct and respects
	// the nuances of map and slice comparison.
	//aKind := reflect.TypeOf(a).Kind()
	//bKind := reflect.TypeOf(b).Kind()
	//if aKind == reflect.Map || aKind == reflect.Slice || bKind == reflect.Map || bKind == reflect.Slice {
	//	return cmp.Equal(a, b)
	//}
	//
	//// For primary types (string, bool, numbers), we use a normalization
	//// step to handle type discrepancies, such as int64 from a CRD vs.
	//// float64 from a JSON response.
	//strA := fmt.Sprintf("%v", a)
	//strB := fmt.Sprintf("%v", b)
	//
	//normA := jqutil.InferType(strA)
	//normB := jqutil.InferType(strB)

	//return cmp.Equal(normA, normB)
	return cmp.Equal(a, b)
}
