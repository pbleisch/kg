package query

import "fmt"

// QueryError represents a structured error from a query operation.
type QueryError struct {
	Code        string
	Message     string
	Suggestions []string
	// Err is the error this one explains, when there is one. It is unwrapped,
	// so a caller can still ask errors.As whether the graph refused to load
	// rather than whether the query failed.
	Err error
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *QueryError) Unwrap() error { return e.Err }
