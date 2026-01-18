package manaxclient

import (
	"io"
	"strings"
	"testing"
)

// TestSSEReader_SingleEvent verifies that sseReader correctly parses
// a single event with "event" and "data" fields.
func TestSSEReader_SingleEvent(t *testing.T) {
	raw := "event: facts\n" +
		"data: {\"foo\":1}\n" +
		"\n"

	r := newSSEReader(strings.NewReader(raw))

	ev, err := r.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent returned error: %v", err)
	}
	if ev.Event != "facts" {
		t.Fatalf("unexpected Event: %q", ev.Event)
	}
	if string(ev.Data) != "{\"foo\":1}" {
		t.Fatalf("unexpected Data: %q", ev.Data)
	}

	// Second call must return io.EOF.
	if _, err := r.ReadEvent(); err != io.EOF {
		t.Fatalf("expected EOF, got: %v", err)
	}
}

// TestSSEReader_MultiLineData ensures that multiple "data:" lines are
// combined using '\n' as required by the SSE specification.
func TestSSEReader_MultiLineData(t *testing.T) {
	raw := "event: facts\n" +
		"data: line1\n" +
		"data: line2\n" +
		"\n"

	r := newSSEReader(strings.NewReader(raw))

	ev, err := r.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent returned error: %v", err)
	}
	if string(ev.Data) != "line1\nline2" {
		t.Fatalf("unexpected combined data: %q", ev.Data)
	}
}

// TestSSEReader_CommentOnly verifies that pure comment events are
// parsed as SSEEvent with Comment set and no Event/Data.
func TestSSEReader_CommentOnly(t *testing.T) {
	raw := ": ping\n\n"

	r := newSSEReader(strings.NewReader(raw))

	ev, err := r.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent returned error: %v", err)
	}
	if ev.Comment != "ping" {
		t.Fatalf("unexpected Comment: %q", ev.Comment)
	}
	if ev.Event != "" || len(ev.Data) != 0 {
		t.Fatalf("expected no Event/Data for comment-only event, got %#v", ev)
	}
}