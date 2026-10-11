package protocol

import (
	"bufio"
	"encoding/json"
	"io"
)

// NewScanner reads one message per line and fails on a line over MaxLine.
func NewScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 4096), MaxLine+1)
	return s
}

// Line encodes v as one message, newline included.
func Line(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
