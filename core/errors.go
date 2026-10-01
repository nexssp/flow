package core

import "fmt"

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

func SourceError(pos Position, format string, args ...any) error {
	return fmt.Errorf("%s: %s", pos.String(), fmt.Sprintf(format, args...))
}
