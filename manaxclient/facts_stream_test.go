package manaxclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestStreamFacts_Basic verifies that StreamFacts:
//   - hits the correct path and query;
//   - parses two SSE "facts" events;
//   - invokes the handler for each event.
func TestStreamFacts_Basic(t *testing.T) {
	// Prepare two chunks (snapshot + update) similar to the real ApiService.
	now := time.Now().UTC()
	chunk1 := FactsStreamChunk{
		ProID:           "p_123",
		CursorUpdatedUTC: now,
		CursorID:        1,
		Items: []FactItem{
			{ID: 1, ProID: "p_123", FactText: "one"},
		},
	}
	chunk2 := FactsStreamChunk{
		ProID:           "p_123",
		CursorUpdatedUTC: now.Add(time.Minute),
		CursorID:        2,
		Items: []FactItem{
			{ID: 2, ProID: "p_123", FactText: "two"},
		},
	}

	handlerHTTP := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/facts/items/stream" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("proId") != "p_123" {
			t.Fatalf("unexpected proId: %v", q)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("unexpected Accept header: %q", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")

		enc := json.NewEncoder(w)

		// Emit a comment keepalive, then two "facts" events.
		w.Write([]byte(": ping\n\n"))

		w.Write([]byte("event: facts\ndata: "))
		if err := enc.Encode(chunk1); err != nil {
			t.Fatalf("encode chunk1 failed: %v", err)
		}
		// json.Encoder.Encode appends '\n', but SSE requires a blank line
		// after the last data line; we add it explicitly.
		w.Write([]byte("\n"))

		w.Write([]byte("event: facts\ndata: "))
		if err := enc.Encode(chunk2); err != nil {
			t.Fatalf("encode chunk2 failed: %v", err)
		}
		w.Write([]byte("\n"))
	}

	srv := httptest.NewServer(http.HandlerFunc(handlerHTTP))
	defer srv.Close()

	client, err := NewClient(srv.URL, nil)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	var got []FactsStreamChunk

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = client.StreamFacts(ctx, "p_123", func(ctx context.Context, chunk *FactsStreamChunk) error {
		got = append(got, *chunk)
		// After reading two events, cancel the context to stop streaming.
		if len(got) == 2 {
			cancel()
		}
		return nil
	})

	// StreamFacts may return context.Canceled; treat it as success here.
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("StreamFacts returned error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}
	if got[0].Items[0].FactText != "one" || got[1].Items[0].FactText != "two" {
		t.Fatalf("unexpected chunks: %#v", got)
	}
}