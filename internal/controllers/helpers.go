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
			convertedValue := convertValueForUnstructured(v)

			if err := unstructured.SetNestedField(mg.Object, convertedValue, "status", k); err != nil {
				return fmt.Errorf("setting nested field '%s' in status: %w", k, err)
			}
		}
	}

	return nil
}

// convertValueForUnstructured converts values to types that can be safely handled by unstructured.SetNestedField
// otherwise the value wouldn't be deep copied correctly into the unstructured object and a panic would occur
// int64 is the standard integer type that Kubernetes unstructured objects
func convertValueForUnstructured(value interface{}) interface{} {
	if value == nil {
		return nil
	}

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
		return v
	case uint:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		// Check if it fits in int64 to avoid overflow
		if v <= math.MaxInt64 {
			return int64(v)
		}
		// If too large for int64, convert to string (fallback)
		return fmt.Sprintf("%d", v)
	case float32:
		f64 := float64(v)
		// Handle special float values that might cause issues (fallback)
		if math.IsInf(f64, 0) || math.IsNaN(f64) {
			return fmt.Sprintf("%f", f64)
		}
		return f64
	case float64:
		// Handle special float values that might cause issues (fallback)
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return fmt.Sprintf("%f", v)
		}
		return v
	case bool:
		return v
	case string:
		return v
	case []interface{}:
		// Recursively convert slice elements
		converted := make([]interface{}, len(v))
		for i, item := range v {
			converted[i] = convertValueForUnstructured(item)
		}
		return converted
	case map[string]interface{}:
		// Recursively convert map values
		converted := make(map[string]interface{})
		for key, val := range v {
			converted[key] = convertValueForUnstructured(val)
		}
		return converted
	default:
		// For any other type, try to convert to string as a fallback
		return fmt.Sprintf("%v", v)
	}
}
