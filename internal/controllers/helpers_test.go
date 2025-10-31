package restResources

import (
	"testing"

	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/google/go-cmp/cmp"
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
						"existing": "existingValue",
					},
				},
			},
			body: map[string]interface{}{
				"id": "123",
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"existing": "existingValue",
					"id":       "123",
				},
			},
		},
		{
			name: "non-strings data types - integers, booleans",
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
		{
			name: "identifiers with mixed types (float and boolean)",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers: []string{"count", "active"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"count":  42.5,
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
		{
			name: "identifiers with mixed types (string, integer, boolean)",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers: []string{"id", "count", "active"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"id":     "123",
				"count":  42,
				"active": true,
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"id":     "123",
					"count":  int64(42),
					"active": true,
				},
			},
		},
		{
			name: "nested identifiers and additional status fields",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers:            []string{"metadata.uid"},
					AdditionalStatusFields: []string{"spec.host", "status.phase"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"metadata": map[string]interface{}{
					"uid": "xyz-123",
				},
				"spec": map[string]interface{}{
					"host": "example.com",
				},
				"status": map[string]interface{}{
					"phase": "Running",
				},
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"metadata": map[string]interface{}{
						"uid": "xyz-123",
					},
					"spec": map[string]interface{}{
						"host": "example.com",
					},
					"status": map[string]interface{}{
						"phase": "Running",
					},
				},
			},
		},
		{
			name: "mixed top-level and nested fields",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					Identifiers:            []string{"id"},
					AdditionalStatusFields: []string{"metadata.name"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"id": "abc-456",
				"metadata": map[string]interface{}{
					"name": "my-resource",
				},
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"id": "abc-456",
					"metadata": map[string]interface{}{
						"name": "my-resource",
					},
				},
			},
		},
		{
			name: "nested field not found in body",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					AdditionalStatusFields: []string{"spec.nonexistent"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"spec": map[string]interface{}{
					"host": "example.com",
				},
			},
			wantErr:  false,
			expected: map[string]interface{}{},
		},
		{
			name: "complex nested object",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					AdditionalStatusFields: []string{"data.config.spec"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"data": map[string]interface{}{
					"config": map[string]interface{}{
						"spec": map[string]interface{}{
							"key": "value",
							"num": float64(123),
						},
					},
				},
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"data": map[string]interface{}{
						"config": map[string]interface{}{
							"spec": map[string]interface{}{
								"key": "value",
								"num": int64(123),
							},
						},
					},
				},
			},
		},
		{
			name: "slice of strings",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					AdditionalStatusFields: []string{"spec.tags"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"spec": map[string]interface{}{
					"tags": []interface{}{"tag1", "tag2"},
				},
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"spec": map[string]interface{}{
						"tags": []interface{}{"tag1", "tag2"},
					},
				},
			},
		},
		{
			name: "slice of objects with mixed numeric types",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					AdditionalStatusFields: []string{"spec.ports"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"spec": map[string]interface{}{
					"ports": []interface{}{
						map[string]interface{}{"name": "http", "port": 80},
						map[string]interface{}{"name": "https", "port": float32(443)},
					},
				},
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"spec": map[string]interface{}{
						"ports": []interface{}{
							map[string]interface{}{"name": "http", "port": int64(80)},
							map[string]interface{}{"name": "https", "port": int64(443)},
						},
					},
				},
			},
		},
		{
			name: "object with nil value",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					AdditionalStatusFields: []string{"config"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"config": map[string]interface{}{
					"settingA": "valueA",
					"settingB": nil,
				},
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"config": map[string]interface{}{
						"settingA": "valueA",
						"settingB": nil,
					},
				},
			},
		},
		{
			name: "slice of objects with mixed numeric types",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					AdditionalStatusFields: []string{"spec.ports"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"spec": map[string]interface{}{
					"ports": []interface{}{
						map[string]interface{}{"name": "http", "port": 80},
						map[string]interface{}{"name": "https", "port": float32(443)},
					},
				},
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"spec": map[string]interface{}{
						"ports": []interface{}{
							map[string]interface{}{"name": "http", "port": int64(80)},
							map[string]interface{}{"name": "https", "port": int64(443)},
						},
					},
				},
			},
		},
		{
			name: "literal dots in field names",
			clientInfo: &getter.Info{
				Resource: getter.Resource{
					AdditionalStatusFields: []string{"root_level.['nested.field.with.dots'].leaf"},
					Identifiers:            []string{"root_level.['another.field.with.dots']"},
				},
			},
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			body: map[string]interface{}{
				"root_level": map[string]interface{}{
					"nested.field.with.dots": map[string]interface{}{
						"leaf": "final_value",
					},
					"another.field.with.dots": "identifier_value",
				},
			},
			wantErr: false,
			expected: map[string]interface{}{
				"status": map[string]interface{}{
					"root_level": map[string]interface{}{
						"nested.field.with.dots": map[string]interface{}{
							"leaf": "final_value",
						},
						"another.field.with.dots": "identifier_value",
					},
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
				if diff := cmp.Diff(tt.expected, tt.mg.Object); diff != "" {
					t.Errorf("populateStatusFields() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// Note: forked from plumbing/maps/deepcopy.go
// modified the float handling
func TestDeepCopyJSONValue(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   any
		mutate func(any)
	}{
		{
			name:  "primitive string",
			input: "hello",
			want:  "hello",
		},
		{
			name:  "primitive int",
			input: 42,
			want:  int64(42),
		},
		{
			name:  "primitive int64",
			input: int64(42),
			want:  int64(42),
		},
		{
			name:  "primitive bool",
			input: true,
			want:  true,
		},
		{
			name:  "primitive float32",
			input: float32(3.14),
			want:  int64(3),
		},
		{
			name:  "primitive float64",
			input: 3.14,
			want:  int64(3),
		},
		{
			name:  "nil",
			input: nil,
			want:  nil,
		},
		{
			name:  "map[string]any",
			input: map[string]any{"foo": "bar"},
			want: func() any {
				return map[string]any{"foo": "bar"}
			}(),
			mutate: func(x any) {
				x.(map[string]any)["foo"] = "baz"
			},
		},
		{
			name: "slice of any",
			input: []any{
				int64(1),
				"a",
				map[string]any{"k": "v"},
			},
			want: func() any {
				return []any{
					int64(1),
					"a",
					map[string]any{"k": "v"},
				}
			}(),
			mutate: func(x any) {
				x.([]any)[2].(map[string]any)["k"] = "changed"
			},
		},
		{
			name: "slice of map[string]any",
			input: []map[string]any{
				{"a": int64(1)},
				{"b": int64(2)},
			},
			want: func() any {
				return []any{
					map[string]any{"a": int64(1)},
					map[string]any{"b": int64(2)},
				}
			}(),
			mutate: func(x any) {
				x.([]map[string]any)[0]["a"] = int64(999)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deepCopyJSONValue(tt.input)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("deepCopyJSONValue() mismatch (-want +got):\n%s", diff)
			}

			if tt.mutate != nil {
				tt.mutate(tt.input)
				if cmp.Equal(tt.input, got) {
					t.Errorf("mutation of input affected the copy")
				}
			}
		})
	}
}
