package manaxclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// MatchesStreamCursor represents the incremental watermark used by
// the matches SSE stream.
//
// The ApiService expects the client to:
//   1. Call /api/matches/items/snapshot and obtain cursorUpdatedUtc / cursorId.
//   2. Pass these values as sinceUpdatedUtc / sinceId when opening the
//      SSE stream.
//   3. For each SSE update chunk, update the cursor and persist it.
type MatchesStreamCursor struct {
	// UpdatedUTC is the last seen CursorUpdatedUtc from either a snapshot
	// or a previous updates chunk.
	UpdatedUTC time.Time

	// ID is the last seen CursorId associated with UpdatedUTC.
	ID int64
}

// MatchesStreamOptions configures additional filters for the matches
// SSE stream. These fields directly map to the MatchingController
// query parameters.
type MatchesStreamOptions struct {
	// Direction is the required matching direction:
	//   - MatchingDirectionOffer ("Offer")
	//   - MatchingDirectionSeek  ("Seek")
	//
	// It is mandatory in SSE stream on the server side.
	Direction MatchingDirection

	// MinScore is an optional lower bound for the match score.
	// Use 0 to disable score filtering on the server side.
	MinScore float64

	// Limit is the maximum number of items returned per update chunk.
	// The server enforces bounds and defaults (e.g. 500).
	// Use 0 to let the server choose the default.
	Limit int

	// MinRationaleLength and MaxRationaleLength restrict the length
	// of the textual rationale field. Bounds are normalized on the
	// server side; 0 means "no bound".
	MinRationaleLength int
	MaxRationaleLength int
}

// MatchesStreamChunk is just an alias of MatchesUpdatesResponse:
// the SSE stream always returns the "updates" shape.
//
// JSON shape:
//   {
//     "proId": "...",
//     "direction": "Offer" | "Seek" | null,
//     "cursorUpdatedUtc": "... RFC3339 ...",
//     "cursorId": 123,
//     "items": [ { /* MatchItem */ }, ... ]
//   }
type MatchesStreamChunk = MatchesUpdatesResponse

// MatchesStreamHandler is a user callback invoked for every decoded
// "matches" event coming from the SSE stream.
//
// The handler is called synchronously from the streaming loop.
// If it returns a non-nil error, the streaming stops and the error
// is propagated back to the caller of StreamMatches.
type MatchesStreamHandler func(ctx context.Context, chunk *MatchesStreamChunk) error

// StreamMatches establishes an SSE connection to
//   GET /api/matches/items/stream
//
// with the following query parameters:
//   - proId            : required.
//   - sinceUpdatedUtc  : initial cursor timestamp (UTC).
//   - sinceId          : initial cursor numeric id.
//   - direction        : required ("Offer" or "Seek").
//   - minScore         : optional.
//   - limit            : optional (capped server-side).
//   - minRationaleLength / maxRationaleLength : optional.
//
// On the server side (MatchingController):
//   - There is no built-in snapshot in the stream; the client is
//     expected to call the /snapshot endpoint first.
//   - The stream polls the database periodically (with pollDelay)
//     and emits "event: matches" with a MatchesUpdatesResponse JSON
//     whenever there are new or changed rows.
//   - When there are no changes, it emits a comment event like
//     ": idle" as a lightweight heartbeat.
//   - On exit it emits a comment ": matches-stream-end".
//
// StreamMatches:
//   - Ignores pure comment events (including keepalives and
//     "matches-stream-start"/"matches-stream-end").
//   - Ignores events whose type is not "matches".
//   - Decodes event data into MatchesStreamChunk (MatchesUpdatesResponse)
//     and passes it to the user handler.
//   - Stops on:
//       * context cancellation;
//       * EOF from the server;
//       * any I/O or JSON decoding error;
//       * non-nil error from the handler.
//
// The caller is responsible for:
//   - Obtaining an initial MatchesItemsResponse from GetMatchesSnapshot,
//     extracting cursorUpdatedUtc / cursorId;
//   - Passing those values as MatchesStreamCursor;
//   - Updating and persisting the cursor for future reconnections.
func (c *Client) StreamMatches(
	ctx context.Context,
	proID string,
	cursor MatchesStreamCursor,
	opt MatchesStreamOptions,
	handler MatchesStreamHandler,
) error {
	proID = strings.TrimSpace(proID)
	if proID == "" {
		return errors.New("StreamMatches: proID must not be empty")
	}
	if handler == nil {
		return errors.New("StreamMatches: handler must not be nil")
	}
	if opt.Direction == "" {
		return errors.New("StreamMatches: Direction must not be empty")
	}
	if cursor.ID < 0 {
		return errors.New("StreamMatches: cursor.ID must be >= 0")
	}

	// Build query string.
	q := url.Values{}
	q.Set("proId", proID)

	// The server normalizes kind to UTC; we ensure it is formatted as RFC3339.
	if cursor.UpdatedUTC.IsZero() {
		return errors.New("StreamMatches: cursor.UpdatedUTC must not be zero")
	}
	q.Set("sinceUpdatedUtc", cursor.UpdatedUTC.UTC().Format(time.RFC3339))
	q.Set("sinceId", strconv.FormatInt(cursor.ID, 10))

	q.Set("direction", string(opt.Direction))

	if opt.MinScore > 0 {
		q.Set("minScore", strconv.FormatFloat(opt.MinScore, 'f', -1, 64))
	}
	if opt.Limit > 0 {
		q.Set("limit", strconv.Itoa(opt.Limit))
	}
	if opt.MinRationaleLength > 0 {
		q.Set("minRationaleLength", strconv.Itoa(opt.MinRationaleLength))
	}
	if opt.MaxRationaleLength > 0 {
		q.Set("maxRationaleLength", strconv.Itoa(opt.MaxRationaleLength))
	}

	req, err := c.newRequest(ctx, http.MethodGet, "/api/matches/items/stream", q, nil)
	if err != nil {
		return fmt.Errorf("StreamMatches: create request: %w", err)
	}

	h := http.Header{}
	h.Set("Accept", "text/event-stream")
	c.applyHeaders(req, h)

	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("StreamMatches: http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

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
				return nil
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("StreamMatches: read SSE event: %w", err)
		}
		if ev == nil {
			continue
		}

		// Ignore comments (: matches-stream-start / : idle / : matches-stream-end).
		if ev.Comment != "" && ev.Event == "" && len(ev.Data) == 0 {
			continue
		}

		if ev.Event != "" && ev.Event != "matches" {
			continue
		}

		if len(ev.Data) == 0 {
			return fmt.Errorf("StreamMatches: received event \"matches\" with empty data payload")
		}

		var chunk MatchesStreamChunk
		if err := json.Unmarshal(ev.Data, &chunk); err != nil {
			return fmt.Errorf("StreamMatches: decode JSON payload: %w", err)
		}

		if err := handler(ctx, &chunk); err != nil {
			return err
		}
	}
}