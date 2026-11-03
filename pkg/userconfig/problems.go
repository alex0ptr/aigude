package userconfig

import (
	"fmt"
	"strings"
)

// Problem is one thing wrong in a User Config file, with its position.
type Problem struct {
	// File is the path of the file as it was read.
	File string
	// Line and Column are 1-based; Line is 0 when the problem has no position in the file.
	Line, Column int
	Message      string
}

// String renders the Problem as file:line:column: message.
func (p Problem) String() string {
	if p.Line == 0 {
		return fmt.Sprintf("%s: %s", p.File, p.Message)
	}
	return fmt.Sprintf("%s:%d:%d: %s", p.File, p.Line, p.Column, p.Message)
}

// Problems are all problems found while reading User Config files.
type Problems []Problem

// Error lists one Problem per line.
func (ps Problems) Error() string {
	lines := make([]string, len(ps))
	for i, p := range ps {
		lines[i] = p.String()
	}
	return strings.Join(lines, "\n")
}
