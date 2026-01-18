package manaxclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// Client is a high-level HTTP client for the Manax ApiService.
//
// It encapsulates:
//   - Base API URL (scheme + host + optional base path).
//   - Underlying HTTP client (connection pooling, timeouts).
//   - Optional "identity" headers X-Pro-Id / X-Pro-Token used by server middleware.
//
// All methods are safe for concurrent use provided that SetAuth is not
// called concurrently with in-flight requests.
type Client struct {
	// baseURL is the parsed base URL for the API, for example:
	//   https://api.manax.pro
	// or
	//   https://manax.pro/manax
	//
	// Only scheme, host and path are used; query and fragment are ignored.
	baseURL *url.URL

	// httpClient is the underlying HTTP client implementation.
	// If nil, http.DefaultClient is used.
	httpClient *http.Client

	// proID is the current logical identity ("profile id") that will
	// be propagated via X-Pro-Id header if non-empty.
	proID string

	// proToken is the current logical "secret" or token that will be
	// propagated via X-Pro-Token header if non-empty.
	proToken string
}

// NewClient constructs a new Client for the given baseURL string.
// Example baseURL values:
//   - "https://api.manax.pro"
//   - "https://manax.pro/manax"
//
// The httpClient parameter may be nil; in that case http.DefaultClient is used.
// The function validates the base URL and returns an error if it is invalid.
func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, errors.New("baseURL must not be empty")
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid baseURL %q: %w", baseURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("baseURL must include scheme and host: %q", baseURL)
	}

	u.RawQuery = ""
	u.Fragment = ""

	c := &Client{
		baseURL:    u,
		httpClient: httpClient,
	}
	return c, nil
}

// SetAuth configures the client to send X-Pro-Id and X-Pro-Token headers
// with every subsequent request (until changed again).
//
// proID is the logical profile identifier (for example: "p_123").
// proToken is the associated secret/token used by ApiService middleware.
//
// This method mutates client state and therefore must not be called in
// parallel with in-flight requests if strict thread-safety is required.
func (c *Client) SetAuth(proID, proToken string) {
	c.proID = strings.TrimSpace(proID)
	c.proToken = strings.TrimSpace(proToken)
}

// BaseURL returns a copy of the base API URL used by the client.
func (c *Client) BaseURL() url.URL {
	return *c.baseURL
}

// HTTPClient returns the underlying HTTP client. If the client is nil,
// http.DefaultClient will be used automatically.
func (c *Client) HTTPClient() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return http.DefaultClient
}

// APIError represents a non-2xx HTTP response from the ApiService.
// It captures the HTTP status code, an error message (if any), and
// the raw response body bytes for further inspection.
type APIError struct {
	// StatusCode is the HTTP status code, e.g. 400, 401, 500.
	StatusCode int

	// Message is a human-readable error message. When possible, it is
	// extracted from JSON field "error"; otherwise it falls back to the
	// raw body content or HTTP status text.
	Message string

	// Body holds the raw response body bytes as returned by the server.
	Body []byte
}

// Error implements the error interface, providing a concise representation
// of the HTTP status and error message.
func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("api error: status=%d message=%q", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("api error: status=%d", e.StatusCode)
}

// newRequest builds an *http.Request for the given method and relative path,
// attaching the provided query parameters and body.
//
// pathOrEndpoint is a relative path such as:
//   - "/api/speech/upload"
//   - "api/facts/items/snapshot"
//
// The function ensures that the final URL is baseURL + path, preserving
// any base path component present in baseURL.
func (c *Client) newRequest(
	ctx context.Context,
	method string,
	pathOrEndpoint string,
	query url.Values,
	body io.Reader,
) (*http.Request, error) {
	if ctx == nil {
		return nil, errors.New("ctx must not be nil")
	}
	if c.baseURL == nil {
		return nil, errors.New("client baseURL is not initialized")
	}

	relPath := strings.TrimSpace(pathOrEndpoint)
	if !strings.HasPrefix(relPath, "/") {
		relPath = "/" + relPath
	}

	u := *c.baseURL
	u.Path = strings.TrimRight(c.baseURL.Path, "/")
	u.Path = path.Join(u.Path, relPath)

	if query != nil {
		u.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	return req, nil
}

// applyHeaders merges base headers (including X-Pro-Id / X-Pro-Token)
// with the provided header set and assigns them to the request.
//
// extra may be nil. If not nil, its contents are copied into a new map
// so that callers are free to reuse their header instances.
func (c *Client) applyHeaders(req *http.Request, extra http.Header) {
	merged := make(http.Header, len(extra)+2)

	for k, vals := range extra {
		dst := make([]string, len(vals))
		copy(dst, vals)
		merged[k] = dst
	}

	if c.proID != "" {
		merged.Set("X-Pro-Id", c.proID)
	}
	if c.proToken != "" {
		merged.Set("X-Pro-Token", c.proToken)
	}
	if merged.Get("Accept") == "" {
		merged.Set("Accept", "application/json")
	}

	req.Header = merged
}

// doJSON executes a prepared HTTP request, validates the response status,
// and if v is non-nil, unmarshals the response JSON into v.
//
// On non-2xx responses, an *APIError is returned.
func (c *Client) doJSON(req *http.Request, v any) error {
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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

	if v == nil || len(data) == 0 {
		return nil
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decode JSON response: %w", err)
	}
	return nil
}

// CreateProWallet issues a POST request to /api/crypto/pro-wallet/create.
//
// This endpoint is responsible for creating a new "pro wallet" on the server
// side, generating a mnemonic phrase (mnemonic24) and an authentication token.
//
// manaxKey, if non-empty, is sent as X-Manax-Key header and may be used
// by the server to authorize privileged operations (e.g. admin-level key).
//
// Returned CreateProWalletResponse is based on the client contract you
// описали ранее: { proId, token, mnemonic24, createdUtc }.
func (c *Client) CreateProWallet(
	ctx context.Context,
	manaxKey string,
) (*CreateProWalletResponse, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/crypto/pro-wallet/create", nil, nil)
	if err != nil {
		return nil, err
	}

	h := http.Header{}
	if strings.TrimSpace(manaxKey) != "" {
		h.Set("X-Manax-Key", strings.TrimSpace(manaxKey))
	}
	c.applyHeaders(req, h)

	var out CreateProWalletResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VerifyProWallet calls GET /api/crypto/pro-wallet/verify with query
// parameters proId and token.
//
// It validates the existence and correctness of a previously issued token.
// The server responds with { proId, valid }, where valid is a boolean
// indicating whether the token is accepted.
func (c *Client) VerifyProWallet(
	ctx context.Context,
	proID string,
	token string,
) (*VerifyProWalletResponse, error) {
	proID = strings.TrimSpace(proID)
	token = strings.TrimSpace(token)

	if proID == "" {
		return nil, errors.New("proID must not be empty")
	}
	if token == "" {
		return nil, errors.New("token must not be empty")
	}

	q := url.Values{}
	q.Set("proId", proID)
	q.Set("token", token)

	req, err := c.newRequest(ctx, http.MethodGet, "/api/crypto/pro-wallet/verify", q, nil)
	if err != nil {
		return nil, err
	}

	c.applyHeaders(req, nil)

	var out VerifyProWalletResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UploadSpeechAudio uploads a single audio chunk via
// POST /api/speech/upload (multipart/form-data).
//
// The request body uses fields:
//   - audio      : binary content of the audio file.
//   - proId      : logical profile id.
//   - sessionId  : logical session id, grouping multiple chunks.
//   - chunkIndex : index of the chunk within the session (0-based).
//   - sampleRate : optional sample rate in Hz.
//
// The server responds with SpeechUploadResponse describing stored paths,
// effective sample rate, transcript (if already available) and other metadata.
func (c *Client) UploadSpeechAudio(
	ctx context.Context,
	in UploadSpeechAudioRequest,
) (*SpeechUploadResponse, error) {
	if in.Audio == nil {
		return nil, errors.New("UploadSpeechAudio: Audio must not be nil")
	}
	if strings.TrimSpace(in.ProID) == "" {
		return nil, errors.New("UploadSpeechAudio: ProID must not be empty")
	}
	if strings.TrimSpace(in.SessionID) == "" {
		return nil, errors.New("UploadSpeechAudio: SessionID must not be empty")
	}
	if in.ChunkIndex < 0 {
		return nil, errors.New("UploadSpeechAudio: ChunkIndex must be >= 0")
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	fileName := in.FileName
	if strings.TrimSpace(fileName) == "" {
		fileName = "audio"
	}
	part, err := writer.CreateFormFile("audio", fileName)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}

	if _, err := io.Copy(part, in.Audio); err != nil {
		return nil, fmt.Errorf("copy audio: %w", err)
	}

	if err := writer.WriteField("proId", strings.TrimSpace(in.ProID)); err != nil {
		return nil, fmt.Errorf("write proId: %w", err)
	}
	if err := writer.WriteField("sessionId", strings.TrimSpace(in.SessionID)); err != nil {
		return nil, fmt.Errorf("write sessionId: %w", err)
	}
	if err := writer.WriteField("chunkIndex", strconv.Itoa(in.ChunkIndex)); err != nil {
		return nil, fmt.Errorf("write chunkIndex: %w", err)
	}
	if in.SampleRate > 0 {
		if err := writer.WriteField("sampleRate", strconv.Itoa(in.SampleRate)); err != nil {
			return nil, fmt.Errorf("write sampleRate: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finalize multipart body: %w", err)
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/api/speech/upload", nil, &buf)
	if err != nil {
		return nil, err
	}

	h := http.Header{}
	h.Set("Content-Type", writer.FormDataContentType())
	c.applyHeaders(req, h)

	var out SpeechUploadResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UploadSpeechText sends a text segment associated with a speech chunk
// using POST /api/speech/text with JSON body:
//
//   {
//     "proId":      "...",
//     "sessionId":  "...",
//     "chunkIndex": 0,
//     "text":       "..."
//   }
//
// The exact response shape depends on the server implementation. This client
// deliberately exposes it as opaque JSON (json.RawMessage) to avoid
// hardcoding speculative fields.
func (c *Client) UploadSpeechText(
	ctx context.Context,
	in UploadSpeechTextRequest,
) (*UploadSpeechTextResponse, error) {
	if strings.TrimSpace(in.ProID) == "" {
		return nil, errors.New("UploadSpeechText: ProID must not be empty")
	}
	if strings.TrimSpace(in.SessionID) == "" {
		return nil, errors.New("UploadSpeechText: SessionID must not be empty")
	}
	if in.ChunkIndex < 0 {
		return nil, errors.New("UploadSpeechText: ChunkIndex must be >= 0")
	}
	if strings.TrimSpace(in.Text) == "" {
		return nil, errors.New("UploadSpeechText: Text must not be empty")
	}

	payload, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("marshal UploadSpeechTextRequest: %w", err)
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/api/speech/text", nil, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	h := http.Header{}
	h.Set("Content-Type", "application/json")
	c.applyHeaders(req, h)

	var raw json.RawMessage
	if err := c.doJSON(req, &raw); err != nil {
		return nil, err
	}

	return &UploadSpeechTextResponse{Raw: raw}, nil
}

// GetSpeechStatusByID calls GET /api/speech/status?id=<id> and returns
// the current ASR status and metadata for the specified row.
//
// This is the most direct way to query a known row by its primary key.
func (c *Client) GetSpeechStatusByID(
	ctx context.Context,
	id int64,
) (*SpeechStatusResponse, error) {
	if id <= 0 {
		return nil, errors.New("GetSpeechStatusByID: id must be > 0")
	}

	q := url.Values{}
	q.Set("id", strconv.FormatInt(id, 10))

	req, err := c.newRequest(ctx, http.MethodGet, "/api/speech/status", q, nil)
	if err != nil {
		return nil, err
	}

	c.applyHeaders(req, nil)

	var out SpeechStatusResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSpeechStatusByKey calls GET /api/speech/status with composite key
// (proId, sessionId, chunkIndex). This is used when the caller does not
// know the internal numeric id but has logical identifiers.
//
// proID is optional from server perspective; if empty, the server may fall
// back to "p_anon" or another default.
func (c *Client) GetSpeechStatusByKey(
	ctx context.Context,
	proID string,
	sessionID string,
	chunkIndex int,
) (*SpeechStatusResponse, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("GetSpeechStatusByKey: sessionID must not be empty")
	}
	if chunkIndex < 0 {
		return nil, errors.New("GetSpeechStatusByKey: chunkIndex must be >= 0")
	}

	q := url.Values{}
	if strings.TrimSpace(proID) != "" {
		q.Set("proId", strings.TrimSpace(proID))
	}
	q.Set("sessionId", sessionID)
	q.Set("chunkIndex", strconv.Itoa(chunkIndex))

	req, err := c.newRequest(ctx, http.MethodGet, "/api/speech/status", q, nil)
	if err != nil {
		return nil, err
	}

	c.applyHeaders(req, nil)

	var out SpeechStatusResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetFactsSnapshot queries GET /api/facts/items/snapshot for the given
// proId and optional limit.
//
// The server returns a window of fact items plus a cursor (updatedUtc, id)
// that can be used with GetFactsUpdates to poll incremental changes.
func (c *Client) GetFactsSnapshot(
	ctx context.Context,
	proID string,
	limit int,
) (*FactsItemsResponse, error) {
	proID = strings.TrimSpace(proID)
	if proID == "" {
		return nil, errors.New("GetFactsSnapshot: proID must not be empty")
	}

	q := url.Values{}
	q.Set("proId", proID)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}

	req, err := c.newRequest(ctx, http.MethodGet, "/api/facts/items/snapshot", q, nil)
	if err != nil {
		return nil, err
	}

	c.applyHeaders(req, nil)

	var out FactsItemsResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetFactsUpdates invokes GET /api/facts/items/updates using the provided
// cursor state and desired limit.
//
// Parameters:
//   - proID           : logical profile id.
//   - sinceUpdatedUtc : timestamp from previous snapshot/updates cursor.
//   - sinceID         : id from previous cursor.
//   - limit           : maximum number of items to return.
//
// The server responds with FactsUpdatesResponse: the new cursor values and
// a collection of FactItem structures representing incremental changes.
func (c *Client) GetFactsUpdates(
	ctx context.Context,
	proID string,
	sinceUpdatedUtc time.Time,
	sinceID int64,
	limit int,
) (*FactsUpdatesResponse, error) {
	proID = strings.TrimSpace(proID)
	if proID == "" {
		return nil, errors.New("GetFactsUpdates: proID must not be empty")
	}
	if sinceID < 0 {
		return nil, errors.New("GetFactsUpdates: sinceID must be >= 0")
	}

	q := url.Values{}
	q.Set("proId", proID)
	if !sinceUpdatedUtc.IsZero() {
		q.Set("sinceUpdatedUtc", sinceUpdatedUtc.UTC().Format(time.RFC3339))
	}
	q.Set("sinceId", strconv.FormatInt(sinceID, 10))
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}

	req, err := c.newRequest(ctx, http.MethodGet, "/api/facts/items/updates", q, nil)
	if err != nil {
		return nil, err
	}

	c.applyHeaders(req, nil)

	var out FactsUpdatesResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PatchFactReviewStatus issues PATCH /api/facts/items/{id}/review-status
// with query parameter proId and JSON body specifying a new review status.
//
// Valid review status values are determined by the server, but from the
// source code they are typically:
//   - "ok"
//   - "not"
//   - null (cleared; represented here by empty string).
func (c *Client) PatchFactReviewStatus(
	ctx context.Context,
	proID string,
	id int64,
	reviewStatus string,
) (*PatchReviewStatusResponse, error) {
	proID = strings.TrimSpace(proID)
	if proID == "" {
		return nil, errors.New("PatchFactReviewStatus: proID must not be empty")
	}
	if id <= 0 {
		return nil, errors.New("PatchFactReviewStatus: id must be > 0")
	}

	q := url.Values{}
	q.Set("proId", proID)

	body := PatchReviewStatusRequest{
		ReviewStatus: strings.TrimSpace(reviewStatus),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal PatchReviewStatusRequest: %w", err)
	}

	endpoint := fmt.Sprintf("/api/facts/items/%d/review-status", id)
	req, err := c.newRequest(ctx, http.MethodPatch, endpoint, q, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	h := http.Header{}
	h.Set("Content-Type", "application/json")
	c.applyHeaders(req, h)

	var out PatchReviewStatusResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMatchesSnapshot calls GET /api/matches/items/snapshot to obtain
// a snapshot of current matches from the matching engine.
//
// Parameters:
//   - proID               : logical profile id.
//   - direction           : "Offer" or "Seek" (see MatchingDirection).
//   - minScore            : optional lower bound for match score.
//   - limit               : max items to return (server may clamp).
//   - minRationaleLength  : minimum length of rationale text (0 = no filter).
//   - maxRationaleLength  : maximum length of rationale (0 = no limit).
func (c *Client) GetMatchesSnapshot(
	ctx context.Context,
	proID string,
	direction MatchingDirection,
	minScore float64,
	limit int,
	minRationaleLength int,
	maxRationaleLength int,
) (*MatchesItemsResponse, error) {
	proID = strings.TrimSpace(proID)
	if proID == "" {
		return nil, errors.New("GetMatchesSnapshot: proID must not be empty")
	}
	if direction == "" {
		return nil, errors.New("GetMatchesSnapshot: direction must not be empty")
	}

	q := url.Values{}
	q.Set("proId", proID)
	q.Set("direction", string(direction))
	if minScore > 0 {
		q.Set("minScore", strconv.FormatFloat(minScore, 'f', -1, 64))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if minRationaleLength > 0 {
		q.Set("minRationaleLength", strconv.Itoa(minRationaleLength))
	}
	if maxRationaleLength > 0 {
		q.Set("maxRationaleLength", strconv.Itoa(maxRationaleLength))
	}

	req, err := c.newRequest(ctx, http.MethodGet, "/api/matches/items/snapshot", q, nil)
	if err != nil {
		return nil, err
	}

	c.applyHeaders(req, nil)

	var out MatchesItemsResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMatchesUpdates calls GET /api/matches/items/updates to retrieve
// incremental updates from the matching engine starting from a cursor.
//
// Parameters:
//   - proID               : logical profile id.
//   - direction           : match direction filter, may be empty to fetch both.
//   - sinceUpdatedUtc     : cursor timestamp from previous snapshot/updates.
//   - sinceID             : cursor id from previous snapshot/updates.
//   - minScore            : optional minimum score.
//   - limit               : maximum number of items to return.
//   - minRationaleLength  : minimum rationale length filter.
//   - maxRationaleLength  : maximum rationale length filter.
func (c *Client) GetMatchesUpdates(
	ctx context.Context,
	proID string,
	direction MatchingDirection,
	sinceUpdatedUtc time.Time,
	sinceID int64,
	minScore float64,
	limit int,
	minRationaleLength int,
	maxRationaleLength int,
) (*MatchesUpdatesResponse, error) {
	proID = strings.TrimSpace(proID)
	if proID == "" {
		return nil, errors.New("GetMatchesUpdates: proID must not be empty")
	}
	if sinceID < 0 {
		return nil, errors.New("GetMatchesUpdates: sinceID must be >= 0")
	}

	q := url.Values{}
	q.Set("proId", proID)
	if direction != "" {
		q.Set("direction", string(direction))
	}
	if !sinceUpdatedUtc.IsZero() {
		q.Set("sinceUpdatedUtc", sinceUpdatedUtc.UTC().Format(time.RFC3339))
	}
	q.Set("sinceId", strconv.FormatInt(sinceID, 10))

	if minScore > 0 {
		q.Set("minScore", strconv.FormatFloat(minScore, 'f', -1, 64))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if minRationaleLength > 0 {
		q.Set("minRationaleLength", strconv.Itoa(minRationaleLength))
	}
	if maxRationaleLength > 0 {
		q.Set("maxRationaleLength", strconv.Itoa(maxRationaleLength))
	}

	req, err := c.newRequest(ctx, http.MethodGet, "/api/matches/items/updates", q, nil)
	if err != nil {
		return nil, err
	}

	c.applyHeaders(req, nil)

	var out MatchesUpdatesResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
