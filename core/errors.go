package core

import (
	"errors"
	"fmt"
)

type Position struct {
	File string
	Line int
	Col  int
}

func (p Position) String() string {
	file := p.File
	if file == "" {
		file = "<input>"
	}
	switch {
	case p.Line <= 0:
		return file
	case p.Col <= 0:
		return fmt.Sprintf("%s:%d", file, p.Line)
	default:
		return fmt.Sprintf("%s:%d:%d", file, p.Line, p.Col)
	}
}

// Positioned is implemented by errors that carry a source position.
// Walk the chain with errors.As to find the first one.
type Positioned interface {
	Position() Position
}

// positionedError wraps any error with a source position. Unwrap returns
// the inner error, so errors.Is / errors.As reach through to the cause.
type positionedError struct {
	pos   Position
	inner error
}

func (e *positionedError) Error() string {
	return e.pos.String() + ": " + e.inner.Error()
}

func (e *positionedError) Unwrap() error      { return e.inner }
func (e *positionedError) Position() Position { return e.pos }

// SourceError creates a compile-time error carrying pos.
// The returned error implements Positioned, so diagnostics can extract
// the exact file:line:col — no string parsing required.
//
// %w in format is passed to fmt.Errorf, so wrapped errors are
// reachable via errors.Is / errors.As as usual.
func SourceError(pos Position, format string, args ...any) error {
	return &positionedError{
		pos:   pos,
		inner: fmt.Errorf(format, args...),
	}
}

// PositionOf walks the unwrap chain of err and returns the first
// Position it finds. The bool is false when no positioned error exists.
func PositionOf(err error) (Position, bool) {
	if err == nil {
		return Position{}, false
	}
	var p Positioned
	if errors.As(err, &p) {
		return p.Position(), true
	}
	return Position{}, false
}
