package restResources

import (
	"fmt"
	"math"

	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/comparison"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// isCRUpdated checks if the CR was updated by comparing the fields in the CR with the response from the API call, if existing cr fields are different from the response, it returns false
func isCRUpdated(mg *unstructured.Unstructured, rm map[string]interface{}) (comparison.ComparisonResult, error) {
	// Handle nil input
	if mg == nil {
		return comparison.ComparisonResult{
			IsEqual: false,
			Reason: &comparison.Reason{
				Reason: "mg is nil",
			},
		}, fmt.Errorf("mg is nil")
	}

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

// populateStatusFields populates the status fields in the mg object with the values from the body
// It checks both the `identifiers` and `additionalStatusFields` defined in the resource
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

	// Early return if no fields to populate
	if len(clientInfo.Resource.Identifiers) == 0 && len(clientInfo.Resource.AdditionalStatusFields) == 0 {
		return nil
	}

	// Create a set of all fields we need to look for
	fieldsToPopulate := make(map[string]struct{})

	// Add identifiers to the set
	for _, identifier := range clientInfo.Resource.Identifiers {
		fieldsToPopulate[identifier] = struct{}{}
	}

	// Add additionalStatusFields to the set
	for _, additionalField := range clientInfo.Resource.AdditionalStatusFields {
		fieldsToPopulate[additionalField] = struct{}{}
	}

	// Single pass through the body map
	for k, v := range body {
		if _, exists := fieldsToPopulate[k]; exists {
			// Convert the value to a format that unstructured can handle
			convertedValue := deepCopyJSONValue(v)

			if err := unstructured.SetNestedField(mg.Object, convertedValue, "status", k); err != nil {
				return fmt.Errorf("setting nested field '%s' in status: %w", k, err)
			}
		}
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
