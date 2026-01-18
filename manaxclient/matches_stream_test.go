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

// TestStreamMatches_Basic verifies that StreamMatches:
//   - sends correct query parameters;
//   - parses "matches" SSE events;
//   - invokes handler correctly.
func TestStreamMatches_Basic(t *testing.T) {
	now := time.Now().UTC()
	chunk := MatchesStreamChunk{
		ProID:           "p_123",
		Direction:       ptrDirection(MatchingDirectionOffer),
		CursorUpdatedUTC: now,
		CursorID:        10,
		Items: []MatchItem{
			{
				ID:          1,
				ProID:       "p_123",
				TargetProID: "p_456",
				Direction:   MatchingDirectionOffer,
				Score:       0.9,
				Rationale:   "test",
				ModelID:     "m1",
				CreatedUTC:  now,
				UpdatedUTC:  now,
			},
		},
	}

	handlerHTTP := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/matches/items/stream" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("proId") != "p_123" {
			t.Fatalf("unexpected proId: %v", q)
		}
		if q.Get("direction") != "Offer" {
			t.Fatalf("unexpected direction: %v", q)
		}
		if q.Get("sinceId") != "5" {
			t.Fatalf("unexpected sinceId: %v", q)
		}
		if q.Get("sinceUpdatedUtc") == "" {
			t.Fatalf("missing sinceUpdatedUtc")
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("unexpected Accept header: %q", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")

		enc := json.NewEncoder(w)

		// Start marker (comment).
		w.Write([]byte(": matches-stream-start\n\n"))

		// Actual event.
		w.Write([]byte("event: matches\ndata: "))
		if err := enc.Encode(chunk); err != nil {
			t.Fatalf("encode failed: %v", err)
		}
		w.Write([]byte("\n"))

		// End marker (comment).
		w.Write([]byte(": matches-stream-end\n\n"))
	}

	srv := httptest.NewServer(http.HandlerFunc(handlerHTTP))
	defer srv.Close()

	client, err := NewClient(srv.URL, nil)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	var got []MatchesStreamChunk

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cursor := MatchesStreamCursor{
		UpdatedUTC: now,
		ID:         5,
	}
	opt := MatchesStreamOptions{
		Direction: MatchingDirectionOffer,
	}

	err = client.StreamMatches(ctx, "p_123", cursor, opt, func(ctx context.Context, chunk *MatchesStreamChunk) error {
		got = append(got, *chunk)
		cancel()
		return nil
	})

	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("StreamMatches returned error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(got))
	}
	if len(got[0].Items) != 1 || got[0].Items[0].TargetProID != "p_456" {
		t.Fatalf("unexpected chunk: %#v", got[0])
	}
}

// ptrDirection is a small helper to construct *MatchingDirection
// for tests without repeating address-of syntax.
func ptrDirection(d MatchingDirection) *MatchingDirection {
	return &d
}