// Package apperr defines typed application errors mapped to HTTP statuses.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is an application error safe to show to the user.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

func newf(status int, code, format string, a ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, a...)}
}

// Forbidden — the user lacks permission.
func Forbidden(format string, a ...any) *Error {
	return newf(http.StatusForbidden, "forbidden", format, a...)
}

// NotFound — the resource does not exist (or is hidden from the user).
func NotFound(format string, a ...any) *Error {
	return newf(http.StatusNotFound, "not_found", format, a...)
}

// BadRequest — invalid input.
func BadRequest(format string, a ...any) *Error {
	return newf(http.StatusBadRequest, "bad_request", format, a...)
}

// Conflict — state does not allow the operation.
func Conflict(format string, a ...any) *Error {
	return newf(http.StatusConflict, "conflict", format, a...)
}

// Unavailable — a dependency is down.
func Unavailable(format string, a ...any) *Error {
	return newf(http.StatusServiceUnavailable, "unavailable", format, a...)
}

// TooMany — rate limit exceeded.
func TooMany(format string, a ...any) *Error {
	return newf(http.StatusTooManyRequests, "rate_limited", format, a...)
}

// As extracts an *Error.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
