package restclient

import (
	"errors"
	"fmt"
	"net/http"
)

type StatusError struct {
	StatusCode int
	Inner      error
}

func (e *StatusError) Error() string {
	if e.Inner != nil {
		return fmt.Sprintf("unexpected status: %d: %v", e.StatusCode, e.Inner)
	}
	return fmt.Sprintf("unexpected status: %d:", e.StatusCode)
}

func (e *StatusError) Unwrap() error {
	return e.Inner
}

// HasStatusErr returns true if err is a ResponseError caused by any of the codes given.
func HasStatusErr(err error, codes ...int) bool {
	if err == nil {
		return false
	}
	if se := new(StatusError); errors.As(err, &se) {
		for _, code := range codes {
			if se.StatusCode == code {
				return true
			}
		}
	}
	return false
}

func HasValidStatusCode(code int, acceptStatuses ...int) bool {
	for _, c := range acceptStatuses {
		if code == c {
			return true
		}
	}
	return false
}

func IsNotFoundError(err error) bool {
	return HasStatusErr(err, http.StatusNotFound)
}
