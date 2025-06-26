package restResources

import (
	"testing"

	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestIsCRUpdated(t *testing.T) {
	tests := []struct {
		name     string
		mg       *unstructured.Unstructured
		rm       map[string]interface{}
		wantErr  bool
		expected bool
	}{
		{
			name: "valid comparison",
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"field1": "value1",
						"field2": "value2",
					},
				},
			},
			rm: map[string]interface{}{
				"field1": "value1",
				"field2": "value2",
			},
			wantErr:  false,
			expected: true,
		},
		{
			name: "missing spec field",
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			rm:      map[string]interface{}{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := isCRUpdated(tt.mg, tt.rm)
			if (err != nil) != tt.wantErr {
				t.Errorf("isCRUpdated() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.IsEqual != tt.expected {
				t.Errorf("isCRUpdated() = %v, want %v", result.IsEqual, tt.expected)
			}
		})
	}
}

func TestPopulateStatusFields(t *testing.T) {
	tests := []struct {
		name       string
		clientInfo *getter.Info
		mg         *unstructured.Unstructured
		body       map[string]interface{}
		wantErr    bool
		expected   map[string]interface{}
	}{
		{
			name: "successful population",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers: []string{"id", "name"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"id":    "123",
				"name":  "test",
				"other": "ignored",
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"id":   "123",
					"name": "test",
				},
			},
		},
		{
			name: "no matching identifiers",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers: []string{"missing"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"id":   "123",
				"name": "test",
			},
			wantErr:  false,
			expected: map[string]interface{}{},
		},
		{
			name: "empty body",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers: []string{"id"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body:     map[string]interface{}{},
			wantErr:  false,
			expected: map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := populateStatusFields(tt.clientInfo, tt.mg, tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("populateStatusFields() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				status, exists, _ := unstructured.NestedMap(tt.mg.Object, "status")
				if len(tt.expected) == 0 {
					if exists {
						t.Errorf("populateStatusFields() unexpected status field created")
					}
				} else {
					expectedStatus := tt.expected["status"].(map[string]interface{})
					for k, v := range expectedStatus {
						if status[k] != v {
							t.Errorf("populateStatusFields() status.%s = %v, want %v", k, status[k], v)
						}
					}
				}
			}
		})
	}
}
