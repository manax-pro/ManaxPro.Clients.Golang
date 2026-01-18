package manaxclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// FactsStreamChunk represents a single "facts" SSE event payload.
//
// The ApiService sends both the initial snapshot and subsequent
// incremental updates using the same JSON shape:
//
//   {
//     "proId": "...",
//     "cursorUpdatedUtc": "... RFC3339 ...",
//     "cursorId": 123,
//     "items": [ { /* FactItem */ }, ... ]
//   }
//
// This type is intentionally defined as an alias of FactsItemsResponse
// to emphasize that the wire format is identical.
type FactsStreamChunk = FactsItemsResponse

// FactsStreamHandler is a user-provided callback invoked for each
// successfully decoded "facts" SSE event.
//
// The handler is called synchronously from the streaming loop.
// If it returns a non-nil error, the streaming stops and the error
// is propagated back to the caller of StreamFacts.
type FactsStreamHandler func(ctx context.Context, chunk *FactsStreamChunk) error

// StreamFacts establishes an SSE connection to
//   GET /api/facts/items/stream?proId=...
//
// and continuously consumes "facts" events, decoding each JSON payload
// into a FactsStreamChunk and passing it to the supplied handler.
//
// Semantics on the server side (FactsController):
//   - On connection start it sends an initial snapshot (full window)
//     as "event: facts".
//   - Then it listens to FactsUpdatesHub for changes and, when
//     notified, sends incremental updates as "event: facts" with the
//     same JSON shape but different cursorUpdatedUtc / cursorId.
//   - Periodically it sends keepalive comments, e.g. ": ping".
//
// StreamFacts:
//   - Ignores pure comment events (keepalives).
//   - Ignores events with a different name than "facts".
//   - Requires non-empty data for each processed event.
//   - Stops on:
//       * context cancellation;
//       * EOF from server;
//       * any I/O or JSON decoding error;
//       * non-nil error returned by handler.
//
// The method is blocking; normally it is invoked either in a dedicated
// goroutine or under a context with cancellation.
func (c *Client) StreamFacts(
	ctx context.Context,
	proID string,
	handler FactsStreamHandler,
) error {
	proID = strings.TrimSpace(proID)
	if proID == "" {
		return errors.New("StreamFacts: proID must not be empty")
	}
	if handler == nil {
		return errors.New("StreamFacts: handler must not be nil")
	}

	// Build query: ?proId=<value>
	q := url.Values{}
	q.Set("proId", proID)

	// Create HTTP request bound to the provided context.
	req, err := c.newRequest(ctx, http.MethodGet, "/api/facts/items/stream", q, nil)
	if err != nil {
		return fmt.Errorf("StreamFacts: create request: %w", err)
	}

	// SSE best practice: explicitly express preference for text/event-stream.
	h := http.Header{}
	h.Set("Accept", "text/event-stream")
	c.applyHeaders(req, h)

	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		// If context has been cancelled, surface context error directly.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("StreamFacts: http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read limited body to avoid unbounded memory usage.
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		// Try to decode JSON error, if any.
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &payload)

		msg := strings.TrimSpace(payload.Error)
		if msg == "" && len(data) > 0 {
			msg = strings.TrimSpace(string(data))
		}
		if msg == "" {
			msg = resp.Status
		}

		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    msg,
			Body:       data,
		}
	}

	reader := newSSEReader(resp.Body)

	for {
		ev, err := reader.ReadEvent()
		if err != nil {
			if errors.Is(err, io.EOF) {
				// Normal termination: server closed the stream.
				// If the caller wants automatic reconnection, they
				// can implement it around this method.
				return nil
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				// Prefer propagating context cancellation error when
				// both a read error and a cancelled context exist.
				return ctxErr
			}
			return fmt.Errorf("StreamFacts: read SSE event: %w", err)
		}
		if ev == nil {
			continue
		}

		// Ignore pure comment events (keepalives, "ping", etc.).
		if ev.Comment != "" && ev.Event == "" && len(ev.Data) == 0 {
			continue
		}

		// Only process "facts" events; ignore any other event types
		// to keep the stream forwards-compatible.
		if ev.Event != "" && ev.Event != "facts" {
			continue
		}

		if len(ev.Data) == 0 {
			// Malformed event: event type without data.
			// Treat as error to avoid silently hiding server bugs.
			return fmt.Errorf("StreamFacts: received event \"facts\" with empty data payload")
		}

		var chunk FactsStreamChunk
		if err := json.Unmarshal(ev.Data, &chunk); err != nil {
			return fmt.Errorf("StreamFacts: decode JSON payload: %w", err)
		}

		if err := handler(ctx, &chunk); err != nil {
			return err
		}
	}
}