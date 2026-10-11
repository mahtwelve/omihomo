package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrTooLarge means a message would exceed MaxLine.
var ErrTooLarge = errors.New("message over the line limit")

// NewScanner reads one message per line and fails on a line over MaxLine.
func NewScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 4096), MaxLine+1)
	return s
}

// Line encodes v as one message, newline included. A message the peer's
// scanner would reject fails with ErrTooLarge instead.
func Line(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(b) > MaxLine {
		return nil, fmt.Errorf("%w: %d bytes, the limit is %d", ErrTooLarge, len(b), MaxLine)
	}
	return append(b, '\n'), nil
}

// Decode reads exactly one JSON value into v. Unknown fields and anything
// after the value, even a stray delimiter, are errors.
func Decode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data after the value")
	}
	return nil
}
