package restclient

import (
	"errors"
	"net/http"
	"testing"
)

func TestStatusError_Error(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		inner      error
		expected   string
	}{
		{
			name:       "With inner error",
			statusCode: http.StatusInternalServerError,
			inner:      errors.New("some error"),
			expected:   "unexpected status: 500: some error",
		},
		{
			name:       "Without inner error",
			statusCode: http.StatusNotFound,
			inner:      nil,
			expected:   "unexpected status: 404:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &StatusError{
				StatusCode: tt.statusCode,
				Inner:      tt.inner,
			}
			if err.Error() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, err.Error())
			}
		})
	}
}

func TestStatusError_Unwrap(t *testing.T) {
	innerErr := errors.New("inner error")
	err := &StatusError{
		StatusCode: http.StatusInternalServerError,
		Inner:      innerErr,
	}

	if unwrapped := errors.Unwrap(err); unwrapped != innerErr {
		t.Errorf("expected %v, got %v", innerErr, unwrapped)
	}
}

func TestHasStatusErr(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		codes    []int
		expected bool
	}{
		{
			name:     "Matching status code",
			err:      &StatusError{StatusCode: http.StatusNotFound},
			codes:    []int{http.StatusNotFound, http.StatusInternalServerError},
			expected: true,
		},
		{
			name:     "Non-matching status code",
			err:      &StatusError{StatusCode: http.StatusBadRequest},
			codes:    []int{http.StatusNotFound, http.StatusInternalServerError},
			expected: false,
		},
		{
			name:     "Nil error",
			err:      nil,
			codes:    []int{http.StatusNotFound},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasStatusErr(tt.err, tt.codes...)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "Is not found error",
			err:      &StatusError{StatusCode: http.StatusNotFound},
			expected: true,
		},
		{
			name:     "Is not a not found error",
			err:      &StatusError{StatusCode: http.StatusInternalServerError},
			expected: false,
		},
		{
			name:     "Nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsNotFoundError(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
