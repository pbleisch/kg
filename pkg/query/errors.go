package query

import "fmt"

// QueryError represents a structured error from a query operation.
type QueryError struct {
	Code        string
	Message     string
	Suggestions []string
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
