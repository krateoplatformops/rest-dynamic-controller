package restResources

import (
	"fmt"

	"github.com/krateoplatformops/rest-dynamic-controller/internal/text"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/comparison"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// isCRUpdated checks if the CR was updated by comparing the fields in the CR with the response from the API call, if existing cr fields are different from the response, it returns false
func isCRUpdated(mg *unstructured.Unstructured, rm map[string]interface{}) (comparison.ComparisonResult, error) {
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
func populateStatusFields(clientInfo *getter.Info, mg *unstructured.Unstructured, body map[string]interface{}) error {
	for k, v := range body {
		for _, identifier := range clientInfo.Resource.Identifiers {
			if k == identifier {
				stringValue, err := text.GenericToString(v)
				if err != nil {
					return fmt.Errorf("converting value to string: %w", err)
				}
				err = unstructured.SetNestedField(mg.Object, stringValue, "status", identifier)
				if err != nil {
					return fmt.Errorf("setting nested field in status: %w", err)
				}
			}
		}
	}

	return nil
}
