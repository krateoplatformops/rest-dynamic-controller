package comparison

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/go-cmp/cmp"
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
	// Iterate over keys in the first map (mg, representing the CR on the cluster)
	for key, value := range mg {
		currentPath := append(path, key)
		pathStr := fmt.Sprintf("%v", currentPath)
		log.Printf("Comparing field at path: %s", pathStr)

		rmValue, ok := rm[key]
		if !ok {
			// Key does not exist in rm, ignore and continue
			// TODO: to be understood if this is the desired behavior
			// Examples:
			// Key [configurationRef] not found in rm, ignoring and continuing (this is desired, but maybe can be whitelisted)
			log.Printf("Key %s not found in rm, ignoring and continuing", pathStr)
			continue
		}

		// Handle case where one or both values are nil
		if value == nil || rmValue == nil {
			if value == nil && rmValue == nil {
				log.Printf("Both values are nil at %s, considered equal for this field", pathStr)
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
			// Here we compare primary types (string, bool, numbers)
			log.Printf("Arrived at default case for key %s with local value '%v' and remote value '%v'", pathStr, value, rmValue)
			ok := CompareAny(value, rmValue)
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

func CompareAny(a any, b any) bool {
	log.Printf("Inside CompareAny - Initial values: '%v' and '%v'\n", a, b)

	if a == nil && b == nil {
		log.Print("Both values are nil, returning true")
		return true
	}
	if a == nil || b == nil {
		log.Print("One value is nil while the other is not, returning false")
		return false
	}

	strA := fmt.Sprintf("%v", a)
	strB := fmt.Sprintf("%v", b)
	log.Printf("Comparing values: '%s' and '%s'\n", strA, strB)

	a = InferType(strA)
	b = InferType(strB)
	log.Printf("Normalized values: '%v' and '%v'\n", a, b)

	log.Printf("Values to compare: '%v' and '%v'\n", a, b)
	diff := cmp.Diff(a, b)
	log.Printf("cmp diff:\n%s", diff)

	return cmp.Equal(a, b)
}

// DeepEqual performs a deep comparison between two values.
// This function is currently used in FindBy identifier comparisons (see isInResource in clienttools.go).
// It is suitable for comparing also complex structures like maps and slices.
// For maps (objects), key order does not matter.
// For slices (arrays), element order and content are strictly compared.
func DeepEqual(a, b interface{}) bool {
	log.Printf("Inisde DeepEqual - Values to compare: '%v' and '%v'\n", a, b)
	// For complex types, a direct recursive comparison is correct and respects
	// the nuances of map and slice comparison.

	if a == nil && b == nil {
		log.Print("Both values are nil, returning true")
		return true
	}
	if a == nil || b == nil {
		log.Print("One value is nil while the other is not, returning false")
		return false
	}

	aKind := reflect.TypeOf(a).Kind()
	bKind := reflect.TypeOf(b).Kind()
	log.Printf("Types of values: aKind=%v, bKind=%v\n", aKind, bKind)
	if aKind == reflect.Map || aKind == reflect.Slice || bKind == reflect.Map || bKind == reflect.Slice {
		log.Printf("Using direct comparison for complex types: '%v' and '%v'\n", a, b)
		diff := cmp.Diff(a, b)
		log.Printf("cmp diff:\n%s", diff)
		return cmp.Equal(a, b)
	}

	// For primary types (string, bool, numbers), we use a normalization
	// step to handle type discrepancies, such as idifferent numeric types for integers and floats.
	strA := fmt.Sprintf("%v", a)
	strB := fmt.Sprintf("%v", b)

	normA := InferType(strA)
	normB := InferType(strB)

	// DEBUG
	log.Print("Inside DeepEqual function, after normalization:")
	log.Printf("Comparing normalized values: '%v' and '%v'\n", normA, normB)

	diff := cmp.Diff(normA, normB)
	log.Printf("cmp diff:\n%s", diff)

	return cmp.Equal(normA, normB)

}

// FORK from plumbing

// InferType attempts to infer and convert a string value to its most appropriate Go type.
// It supports primitive types (bool, int32, int64, float64, string), as well as
// structured types commonly found in Kubernetes configurations (map[string]any and []any).
// The function first tries to parse the input as JSON. If that fails, it falls back to
// custom parsing logic for booleans, nil/null, integers, and floats.
// If no conversion is possible, the original string is returned.
func InferType(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}

	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()

	var jsonVal any
	if err := decoder.Decode(&jsonVal); err == nil {
		// Check if there's more data after what was decoded
		// This ensures we only accept values where the entire string is valid JSON
		if decoder.More() {
			// There's more data, so this isn't a complete JSON value
			// E.g., UUID that starts with numbers like: "90f9629b-664b-4804-a560-dd79b0c628f8"
			// Decoder will parse "90" as a number and leave the rest which is not desired
			// Fall through to other parsing attempts
		} else {
			switch v := jsonVal.(type) {
			case json.Number:
				if i, err := v.Int64(); err == nil {
					if i >= math.MinInt32 && i <= math.MaxInt32 {
						return int32(i)
					}
					return i
				}
				if f, err := v.Float64(); err == nil {
					return f
				}
			default:
				return jsonVal
			}
		}
	}

	if strings.EqualFold(value, "true") {
		return true
	}
	if strings.EqualFold(value, "false") {
		return false
	}

	if strings.EqualFold(value, "null") || strings.EqualFold(value, "nil") {
		return nil
	}

	if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
		if intVal >= math.MinInt32 && intVal <= math.MaxInt32 {
			return int32(intVal)
		}
		return intVal
	}

	if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
		if floatVal == math.Trunc(floatVal) {
			if floatVal >= math.MinInt64 && floatVal <= math.MaxInt64 {
				return int64(floatVal)
			}
		}
		return floatVal
	}

	return value
}
