package security

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Validation limits.
const (
	DefaultMaxMessageSize = 1 << 20 // 1 MiB
	DefaultMaxJSONDepth   = 32      // reasonable nesting limit
)

// Validation errors.
var (
	ErrMessageTooLarge = errors.New("message exceeds maximum size")
	ErrJSONTooDeep     = errors.New("JSON nesting exceeds maximum depth")
	ErrInvalidJSON     = errors.New("invalid JSON")
)

// ValidateMessageSize checks that data does not exceed limit bytes.
// If limit is <= 0, DefaultMaxMessageSize is used.
func ValidateMessageSize(data []byte, limit int) error {
	if limit <= 0 {
		limit = DefaultMaxMessageSize
	}
	if len(data) > limit {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrMessageTooLarge, len(data), limit)
	}
	return nil
}

// ValidateJSONDepth checks that the JSON in data does not nest deeper
// than limit levels. This protects against JSON bombs that could exhaust
// stack or memory. If limit is <= 0, DefaultMaxJSONDepth is used.
func ValidateJSONDepth(data []byte, limit int) error {
	if limit <= 0 {
		limit = DefaultMaxJSONDepth
	}
	if len(data) == 0 {
		return nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	depth := 0

	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("%w: %w", ErrInvalidJSON, err)
		}

		switch tok {
		case json.Delim('{'), json.Delim('['):
			depth++
			if depth > limit {
				return fmt.Errorf("%w: depth %d (max %d)", ErrJSONTooDeep, depth, limit)
			}
		case json.Delim('}'), json.Delim(']'):
			depth--
		}
	}
}
