package restResources

import (
	"fmt"
	"math"
	"strings"

	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/comparison"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// isCRUpdated checks if the CR was updated by comparing the fields in the CR with the response from the API call, if existing cr fields are different from the response, it returns false
func isCRUpdated(mg *unstructured.Unstructured, rm map[string]interface{}) (comparison.ComparisonResult, error) {
	if mg == nil {
		return comparison.ComparisonResult{
			IsEqual: false,
			Reason: &comparison.Reason{
				Reason: "mg is nil",
			},
		}, fmt.Errorf("mg is nil")
	}

	// Extract the "spec" fields from the mg object
	m, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		return comparison.ComparisonResult{
			IsEqual: false,
			Reason: &comparison.Reason{
				Reason: "getting spec fields",
			},
		}, fmt.Errorf("getting spec fields: %w", err)
	}

	return comparison.CompareExisting(m, rm)
}

// populateStatusFields populates the status fields in the mg object with the values from the response body of the API call.
// It supports dot notation for nested fields and performs necessary type conversions.
// It uses the identifiers and additionalStatusFields from the clientInfo to determine which fields to populate.
func populateStatusFields(clientInfo *getter.Info, mg *unstructured.Unstructured, body map[string]interface{}) error {
	// Handle nil inputs
	if mg == nil {
		return fmt.Errorf("unstructured object is nil")
	}
	if clientInfo == nil {
		return fmt.Errorf("client info is nil")
	}
	if body == nil {
		return nil // Nothing to populate, but not an error
	}

	// Combine identifiers and additionalStatusFields into one list.
	allFields := append(clientInfo.Resource.Identifiers, clientInfo.Resource.AdditionalStatusFields...)
	// Early return if no fields to populate
	if len(allFields) == 0 {
		return nil
	}

	for _, fieldName := range allFields {
		//log.Printf("Managing field: %s", fieldName)
		// Split the field name by '.' to handle nested paths.
		path := strings.Split(fieldName, ".")
		//log.Printf("Field path split: %v", path)

		// Extract the raw value from the response body without copying.
		value, found, err := unstructured.NestedFieldNoCopy(body, path...)
		if err != nil || !found {
			// An error here means the path was invalid or not found.
			// We can safely continue to the next field.
			//log.Printf("Field '%s' not found in response body or error occurred: %v", fieldName, err)
			continue
		}
		//log.Printf("Extracted value for field '%s': %v", fieldName, value)

		// Perform deep copy and type conversions (e.g., float64 to int64).
		convertedValue := deepCopyJSONValue(value)
		//log.Printf("Converted value for field '%s': %v", fieldName, convertedValue)

		// The destination path in the status should mirror the source path.
		statusPath := append([]string{"status"}, path...)
		//log.Printf("Setting value for field '%s' at status path: %v", fieldName, statusPath)
		if err := unstructured.SetNestedField(mg.Object, convertedValue, statusPath...); err != nil {
			return fmt.Errorf("setting nested field '%s' in status: %w", fieldName, err)
		}
		//log.Printf("Successfully set field '%s' with value: %v at path: %v", fieldName, convertedValue, statusPath)
	}

	return nil
}

// Note: forked from plumbing/maps/deepcopy.go
// modified the float handling
func deepCopyJSONValue(x any) any {
	switch x := x.(type) {
	case map[string]any:
		if x == nil {
			// Typed nil - an any that contains a type map[string]any with a value of nil
			return x
		}
		clone := make(map[string]any, len(x))
		for k, v := range x {
			clone[k] = deepCopyJSONValue(v)
		}
		return clone
	case []any:
		if x == nil {
			// Typed nil - an any that contains a type []any with a value of nil
			return x
		}
		clone := make([]any, len(x))
		for i, v := range x {
			clone[i] = deepCopyJSONValue(v)
		}
		return clone
	case []map[string]any:
		if x == nil {
			return x
		}
		clone := make([]any, len(x))
		for i, v := range x {
			clone[i] = deepCopyJSONValue(v)
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
