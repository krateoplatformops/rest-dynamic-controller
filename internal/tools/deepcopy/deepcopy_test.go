package deepcopy

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

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
			got := DeepCopyJSONValue(tt.input)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("DeepCopyJSONValue() mismatch (-want +got):\n%s", diff)
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
