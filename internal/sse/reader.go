// Package sse provides a minimal Server-Sent Events reader.
package sse

import (
	"bufio"
	"io"
	"strings"
)

// Event is a single parsed SSE event.
type Event struct {
	Type string // "event:" value; empty when not specified
	Data string // concatenated "data:" lines
	ID   string // "id:" value
}

// Reader reads SSE events from an io.Reader.
type Reader struct {
	scanner *bufio.Scanner
}

// NewReader creates a Reader that reads from r.
func NewReader(r io.Reader) *Reader {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024) // up to 1 MiB lines
	return &Reader{scanner: s}
}

// Next returns the next complete SSE event. It returns io.EOF when the
// underlying reader is exhausted.
func (r *Reader) Next() (*Event, error) {
	var (
		evt     Event
		hasData bool
		dataBuf strings.Builder
	)

	for r.scanner.Scan() {
		line := r.scanner.Text()

		// An empty line signals the end of the current event.
		if line == "" {
			if hasData {
				evt.Data = dataBuf.String()
				return &evt, nil
			}
			continue
		}

		// Lines starting with ':' are comments — ignore them.
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, _ := strings.Cut(line, ":")
		// Per spec the first space after ':' is stripped.
		value = strings.TrimPrefix(value, " ")

		switch field {
		case "event":
			evt.Type = value
		case "data":
			if hasData {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(value)
			hasData = true
		case "id":
			evt.ID = value
		}
		// "retry" and unknown fields are silently ignored.
	}

	if err := r.scanner.Err(); err != nil {
		return nil, err
	}

	// Leftover data at EOF without a trailing blank line.
	if hasData {
		evt.Data = dataBuf.String()
		return &evt, nil
	}

	return nil, io.EOF
}
