package manaxclient

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

// SSEEvent represents a single Server-Sent Event as defined by
// the HTML5 EventSource / SSE specification.
//
// The ApiService for Manax uses the following fields:
//   - "event" : logical event name (e.g. "facts", "matches").
//   - "data"  : single or multi-line JSON payload.
//   - comment : lines starting with ":" are comments / keepalives.
//
// This struct supports the full spec (id, retry) even if ApiService
// does not currently use all of them.
type SSEEvent struct {
	// Event is the logical event type, e.g. "facts" or "matches".
	// Empty for pure comment events.
	Event string

	// Data contains the concatenated "data:" lines separated by '\n'.
	// The Manax ApiService always sends a single JSON object here.
	Data []byte

	// ID is the optional event id ("id:" field). It is not currently
	// used by ApiService but kept for completeness.
	ID string

	// Retry holds the optional "retry" value in milliseconds, if the
	// server provided it. The client library does not interpret this
	// value by default.
	Retry string

	// Comment is non-empty when the event represents a comment-only
	// SSE message (line starting with ":"). For example:
	//   : ping
	// or:
	//   : matches-stream-start
	Comment string
}

// sseReader is a low-level incremental parser for Server-Sent Events.
// It reads from an underlying io.Reader and emits SSEEvent instances
// one by one, following the standard SSE framing rules.
type sseReader struct {
	// r is a buffered reader used to read lines efficiently.
	r *bufio.Reader
}

// newSSEReader constructs an SSE reader from an arbitrary io.Reader.
// The caller is responsible for closing the underlying stream.
func newSSEReader(r io.Reader) *sseReader {
	return &sseReader{
		r: bufio.NewReader(r),
	}
}

// ReadEvent reads the next SSE event from the underlying stream.
//
// It returns io.EOF when the stream is closed and no more events
// are available. Partial events (incomplete frame at EOF) are
// ignored and io.EOF is returned.
//
// The function is blocking until one of the following happens:
//   - a complete event is read;
//   - the underlying reader returns an error;
//   - EOF is reached.
//
// The function does not implement any timeout or cancellation logic
// by itself; instead, the caller is expected to use an http.Request
// with context (ctx) when opening the SSE connection so that the
// network read is aborted when the context is cancelled.
func (sr *sseReader) ReadEvent() (*SSEEvent, error) {
	var (
		event     SSEEvent
		dataBuf   bytes.Buffer
		hasData   bool
		hasFields bool
		hasLines  bool
	)

	for {
		line, err := sr.r.ReadString('\n')
		if err != nil {
			// At EOF: if we never saw any content for this event,
			// propagate io.EOF directly. If we have some partial
			// content, treat it as EOF without emitting a partial event.
			if err == io.EOF && !hasLines {
				return nil, io.EOF
			}
			if err == io.EOF {
				return nil, io.EOF
			}
			return nil, err
		}

		// Strip trailing CR/LF.
		line = strings.TrimRight(line, "\r\n")

		// Empty line denotes end of the current event.
		if line == "" {
			if !hasLines {
				// Ignore multiple consecutive blank lines.
				continue
			}
			break
		}

		hasLines = true

		// Comment line: starts with ":" and contains no field.
		if strings.HasPrefix(line, ":") {
			comment := strings.TrimSpace(line[1:])
			// Only treat as comment-only event if we saw no other
			// fields or data. Otherwise ignore inline comments.
			if !hasFields && !hasData {
				event.Comment = comment
			}
			continue
		}

		// Field line: "name:value" or "name".
		var field, value string
		if idx := strings.IndexByte(line, ':'); idx >= 0 {
			field = line[:idx]
			value = line[idx+1:]
			if len(value) > 0 && value[0] == ' ' {
				value = value[1:]
			}
		} else {
			field = line
			value = ""
		}

		switch field {
		case "event":
			event.Event = value
			hasFields = true
		case "data":
			if dataBuf.Len() > 0 {
				// Multiple "data:" lines are joined using '\n'
				// according to the SSE specification.
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(value)
			hasData = true
		case "id":
			event.ID = value
			hasFields = true
		case "retry":
			event.Retry = value
			hasFields = true
		default:
			// Unknown field types are ignored.
		}
	}

	if hasData {
		event.Data = dataBuf.Bytes()
	}

	// If we somehow accumulated no lines, treat it as EOF.
	if !hasLines {
		return nil, io.EOF
	}

	return &event, nil
}