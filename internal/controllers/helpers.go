package restResources

import (
	"fmt"

	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/client/apiaction"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/comparison"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/deepcopy"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/pathparsing"
	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// isCRUpdated checks if the CR was updated by comparing the fields in the CR with the response from the API call, if existing cr fields are different from the response, it returns false
func isCRUpdated(mg *unstructured.Unstructured, rm map[string]interface{}) (comparison.ComparisonResult, error) {
	//log.Print("isCRUpdated - starting comparison between mg spec and rm")
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

	// Debug prints
	//log.Print("isCRUpdated - comparing mg spec with rm")
	// print the mg spec for debugging
	//log.Print("mg spec fields:")
	//for k, v := range m {
	//	log.Printf("mg spec field: %s = %v", k, v)
	//}

	if err != nil {
		return comparison.ComparisonResult{
			IsEqual: false,
			Reason: &comparison.Reason{
				Reason: "getting spec fields",
			},
		}, fmt.Errorf("getting spec fields: %w", err)
	}

	// Debug prints
	//log.Print("rm fields:")
	//for k, v := range rm {
	//	log.Printf("rm field: %s = %v", k, v)
	//}

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
		// Parse the field name into path segments.
		pathSegments, err := pathparsing.ParsePath(fieldName)
		if err != nil || len(pathSegments) == 0 {
			continue
		}

		// Extract the raw value from the response body.
		value, found, err := unstructured.NestedFieldNoCopy(body, pathSegments...)
		if err != nil || !found {
			// An error here means the path was invalid or not found.
			// We can safely continue to the next field.
			continue
		}

		// Perform deep copy and type conversions (e.g., float64 to int64).
		convertedValue := deepcopy.DeepCopyJSONValue(value)

		// The destination path in the status should mirror the source path.
		statusPath := append([]string{"status"}, pathSegments...)
		if err := unstructured.SetNestedField(mg.Object, convertedValue, statusPath...); err != nil {
			return fmt.Errorf("setting nested field '%s' in status: %w", fieldName, err)
		}
	}
	return nil
}

// hasFindByAction checks if the RestDefinition has a findby action configured.
func hasFindByAction(info *getter.Info) bool {
	if info == nil || info.Resource.VerbsDescription == nil {
		return false
	}

	for _, verb := range info.Resource.VerbsDescription {
		if verb.Action == string(apiaction.FindBy) {
			return true
		}
	}

	return false
}
