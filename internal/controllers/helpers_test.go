package restResources

import (
	"math"
	"reflect"
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
			name: "identical values - should be equal",
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
			name: "different values - should not be equal",
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
				"field2": "different_value",
			},
			wantErr:  false,
			expected: false,
		},
		{
			name: "missing fields in rm - should be equal (only existing fields compared)",
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
			},
			wantErr:  false,
			expected: true,
		},
		{
			name: "extra fields in rm - should be equal (only existing fields compared)",
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"field1": "value1",
					},
				},
			},
			rm: map[string]interface{}{
				"field1": "value1",
				"field2": "extra_value",
			},
			wantErr:  false,
			expected: true,
		},
		{
			name: "empty spec",
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{},
				},
			},
			rm:       map[string]interface{}{},
			wantErr:  false,
			expected: true,
		},
		{
			name: "nested objects",
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"nested": map[string]interface{}{
							"field1": "value1",
						},
					},
				},
			},
			rm: map[string]interface{}{
				"nested": map[string]interface{}{
					"field1": "value1",
				},
			},
			wantErr:  false,
			expected: true,
		},
		{
			name: "nested objects - different values",
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"nested": map[string]interface{}{
							"field1": "value1",
						},
					},
				},
			},
			rm: map[string]interface{}{
				"nested": map[string]interface{}{
					"field1": "different_value",
				},
			},
			wantErr:  false,
			expected: false,
		},
		//{
		//	name: "nested objects - different nesting structure", // ???
		//	mg: &unstructured.Unstructured{
		//		Object: map[string]interface{}{
		//			"spec": map[string]interface{}{
		//				"nested": map[string]interface{}{
		//					"field1": "value1",
		//				},
		//			},
		//		},
		//	},
		//	rm: map[string]interface{}{
		//		"nested": map[string]interface{}{
		//			"inner": map[string]interface{}{
		//				"field1": "value1",
		//			},
		//		},
		//	},
		//	wantErr:  false,
		//	expected: false,
		//},
		{
			name: "missing spec field - should error",
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			rm:      map[string]interface{}{},
			wantErr: true,
		},
		{
			name:    "nil mg - should error",
			mg:      nil,
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
			name: "identifiers only - successful population",
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
			name: "additional status fields only",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					AdditionalStatusFields: []string{"status1", "status2"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"status1": "active",
				"status2": "ready",
				"ignored": "field",
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"status1": "active",
					"status2": "ready",
				},
			},
		},
		{
			name: "both identifiers and additional status fields",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers:            []string{"id"},
					AdditionalStatusFields: []string{"state", "version"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"id":      "456",
				"state":   "running",
				"version": "1.0.0",
				"extra":   "ignored",
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"id":      "456",
					"state":   "running",
					"version": "1.0.0",
				},
			},
		},
		{
			name: "overlapping identifiers and additional status fields - should not duplicate",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers:            []string{"id", "name"},
					AdditionalStatusFields: []string{"name", "status"}, // "name" appears in both
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"id":     "789",
				"name":   "test-overlap",
				"status": "active",
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"id":     "789",
					"name":   "test-overlap",
					"status": "active",
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
		{
			name: "no identifiers or additional fields",
			clientInfo: &getter.Info{
				Resource: getter.Resource{},
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
			name: "existing status fields should be preserved",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers: []string{"id"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"status": map[string]interface{}{
						"existing": "value",
					},
				},
			},
			body: map[string]interface{}{
				"id": "123",
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"existing": "value",
					"id":       "123",
				},
			},
		},
		{
			name: "non-strings data types - numbers, booleans",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers:            []string{"id"},
					AdditionalStatusFields: []string{"count", "active"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"id":     42,
				"count":  100,
				"active": true,
				//"tags":   []string{"tag1", "tag2"}, // currently, arrays are not supported in status fields
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"id":     int64(42),
					"count":  int64(100),
					"active": true,
				},
			},
		},
		{
			name: "nil body",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers: []string{"id"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body:     nil,
			wantErr:  false,
			expected: map[string]interface{}{},
		},
		{
			name: "nil mg - should error",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers: []string{"id"},
				},
			},
			mg:      nil,
			body:    map[string]interface{}{"id": "123"},
			wantErr: true,
		},
		{
			name:       "nil clientInfo - should error",
			clientInfo: nil,
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body:    map[string]interface{}{"id": "123"},
			wantErr: true,
		},
		{
			name: "identifiers with mixed types (string and integers)",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers: []string{"id", "count"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"id":    "123",
				"count": 42,
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"id":    "123",
					"count": int64(42),
				},
			},
		},
		{
			name: "identifiers with mixed types (string and boolean)",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers: []string{"id", "active"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"id":     "123",
				"active": true,
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"id":     "123",
					"active": true,
				},
			},
		},
		{
			name: "identifiers with mixed types (integer and boolean)",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers: []string{"count", "active"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"count":  42,
				"active": true,
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"count":  int64(42),
					"active": true,
				},
			},
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
				// Validate the results
				if len(tt.expected) == 0 {
					// No status should be created or should be empty
					status, exists, _ := unstructured.NestedMap(tt.mg.Object, "status")
					if exists && len(status) > 0 {
						// Check if there were pre-existing status fields ("existing" field in status)
						hasPreExisting := false
						for _, test := range tests {
							if test.name == tt.name {
								if statusObj, ok := test.mg.Object["status"]; ok {
									if statusMap, ok := statusObj.(map[string]interface{}); ok && len(statusMap) > 0 {
										hasPreExisting = true
										break
									}
								}
							}
						}
						if !hasPreExisting {
							t.Errorf("populateStatusFields() unexpected status field created: %v while expected is empty", status)
						}
					}
				} else {
					// Validate expected status fields
					status, exists, _ := unstructured.NestedMap(tt.mg.Object, "status")
					if !exists {
						t.Errorf("populateStatusFields() status field not created while length of expected is %d", len(tt.expected))
						return
					}

					expectedStatus := tt.expected["status"].(map[string]interface{})

					// Check that all expected fields are present with correct values
					for k, expectedVal := range expectedStatus {
						if actualVal, ok := status[k]; !ok {
							t.Errorf("populateStatusFields() status.%s not found", k)
						} else if actualVal != expectedVal {
							t.Errorf("populateStatusFields() status.%s = %v, want %v", k, actualVal, expectedVal)
						}
					}

					// For tests with existing status, ensure they're preserved
					if tt.name == "existing status fields should be preserved" {
						if status["existing"] != "value" {
							t.Errorf("populateStatusFields() existing status field not preserved")
						}
					}
				}
			}
		})
	}
}

func TestConvertValueForUnstructured(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "string",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "integer",
			input:    123,
			expected: int64(123),
		},
		{
			name:     "int8",
			input:    int8(10),
			expected: int64(10),
		},
		{
			name:     "int16",
			input:    int16(1000),
			expected: int64(1000),
		},
		{
			name:     "int32",
			input:    int32(100000),
			expected: int64(100000),
		},
		{
			name:     "int64",
			input:    int64(10000000000),
			expected: int64(10000000000),
		},
		{
			name:     "uint",
			input:    uint(50),
			expected: int64(50),
		},
		{
			name:     "uint8",
			input:    uint8(200),
			expected: int64(200),
		},
		{
			name:     "uint16",
			input:    uint16(50000),
			expected: int64(50000),
		},
		{
			name:     "uint32",
			input:    uint32(4000000000),
			expected: int64(4000000000),
		},
		{
			name:     "uint64 - fits in int64",
			input:    uint64(math.MaxInt64),
			expected: int64(math.MaxInt64),
		},
		{
			name:     "uint64 - overflows int64",
			input:    uint64(math.MaxInt64 + 1),
			expected: "9223372036854775808", // fmt.Sprintf("%d", uint64(math.MaxInt64 + 1))
		},
		{
			name:     "float32",
			input:    float32(3.14),
			expected: float64(3.140000104904175),
		},
		{
			name:     "float32 - positive infinity",
			input:    float32(math.Inf(1)),
			expected: "+Inf",
		},
		{
			name:     "float32 - negative infinity",
			input:    float32(math.Inf(-1)),
			expected: "-Inf",
		},
		{
			name:     "float32 - NaN",
			input:    float32(math.NaN()),
			expected: "NaN",
		},
		{
			name:     "float64",
			input:    3.1415926535,
			expected: 3.1415926535,
		},
		{
			name:     "float64 - positive infinity",
			input:    math.Inf(1),
			expected: "+Inf",
		},
		{
			name:     "float64 - negative infinity",
			input:    math.Inf(-1),
			expected: "-Inf",
		},
		{
			name:     "float64 - NaN",
			input:    math.NaN(),
			expected: "NaN",
		},
		{
			name:     "boolean",
			input:    true,
			expected: true,
		},
		{
			name:     "empty slice",
			input:    []interface{}{},
			expected: []interface{}{},
		},
		{
			name:     "slice of strings",
			input:    []interface{}{"a", "b", "c"},
			expected: []interface{}{"a", "b", "c"},
		},
		{
			name:     "slice of integers",
			input:    []interface{}{1, 2, 3},
			expected: []interface{}{int64(1), int64(2), int64(3)},
		},
		{
			name:     "slice of mixed types",
			input:    []interface{}{"a", 1, true, 3.14},
			expected: []interface{}{"a", int64(1), true, 3.14},
		},
		{
			name: "nested slice",
			input: []interface{}{
				"outer",
				[]interface{}{"inner1", 10},
				[]interface{}{"inner2", true},
			},
			expected: []interface{}{
				"outer",
				[]interface{}{"inner1", int64(10)},
				[]interface{}{"inner2", true},
			},
		},
		{
			name: "slice containing map",
			input: []interface{}{
				"item1",
				map[string]interface{}{
					"key1": "value1",
					"key2": 123,
				},
			},
			expected: []interface{}{
				"item1",
				map[string]interface{}{
					"key1": "value1",
					"key2": int64(123),
				},
			},
		},
		{
			name: "map containing slice",
			input: map[string]interface{}{
				"list": []interface{}{"a", 1, true},
				"name": "test",
			},
			expected: map[string]interface{}{
				"list": []interface{}{"a", int64(1), true},
				"name": "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := convertValueForUnstructured(tt.input)
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("convertValueForUnstructured(%v) = %v, want %v", tt.input, actual, tt.expected)
			}
		})
	}
}
