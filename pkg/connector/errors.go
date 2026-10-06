package connector

import "fmt"

// SourceError wraps failures talking to a provider's public source.
type SourceError struct {
	Connector string
	Err       error
}

func (e *SourceError) Error() string {
	return fmt.Sprintf("connector %s: %v", e.Connector, e.Err)
}

func (e *SourceError) Unwrap() error { return e.Err }
