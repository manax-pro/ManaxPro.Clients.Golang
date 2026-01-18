package manaxclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newTestClient builds a Client that talks to the provided httptest.Server.
// It rewrites the base URL accordingly.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()

	srv := httptest.NewServer(handler)

	c, err := NewClient(srv.URL, nil)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	return c, srv
}

// TestNewClient_ValidBaseURL verifies that NewClient accepts a valid base URL
// and constructs a usable Client instance.
func TestNewClient_ValidBaseURL(t *testing.T) {
	c, err := NewClient("https://manax.pro/api", nil)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	u := c.BaseURL()
	if u.Scheme != "https" || u.Host != "manax.pro" {
		t.Fatalf("unexpected BaseURL: %#v", u)
	}
}

// TestNewClient_InvalidBaseURL ensures that invalid base URLs are rejected
// with a proper error.
func TestNewClient_InvalidBaseURL(t *testing.T) {
	if _, err := NewClient("://bad-url", nil); err == nil {
		t.Fatalf("expected error for invalid baseURL, got nil")
	}
}

// TestCreateProWallet verifies that CreateProWallet issues a POST request
// to the correct path with X-Manax-Key and parses the JSON response.
func TestCreateProWallet(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/crypto/pro-wallet/create" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.Header.Get("X-Manax-Key"); got != "admin-key" {
			t.Fatalf("expected X-Manax-Key=admin-key, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		io := CreateProWalletResponse{
			ProID:      "p_123",
			Token:      "tok_abc",
			Mnemonic24: "word1 word2 ... word24",
			CreatedUTC: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		_ = json.NewEncoder(w).Encode(io)
	}

	client, server := newTestClient(t, handler)
	defer server.Close()

	resp, err := client.CreateProWallet(context.Background(), "admin-key")
	if err != nil {
		t.Fatalf("CreateProWallet returned error: %v", err)
	}

	if resp.ProID != "p_123" || resp.Token != "tok_abc" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

// TestVerifyProWallet verifies that VerifyProWallet constructs the correct
// query string and parses the JSON body.
func TestVerifyProWallet(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/crypto/pro-wallet/verify" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("proId") != "p_123" || q.Get("token") != "tok_abc" {
			t.Fatalf("unexpected query: %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(VerifyProWalletResponse{
			ProID: "p_123",
			Valid: true,
		})
	}

	client, server := newTestClient(t, handler)
	defer server.Close()

	resp, err := client.VerifyProWallet(context.Background(), "p_123", "tok_abc")
	if err != nil {
		t.Fatalf("VerifyProWallet returned error: %v", err)
	}
	if !resp.Valid {
		t.Fatalf("expected Valid=true, got false")
	}
}

// TestUploadSpeechAudio validates multipart/form-data construction and
// parsing of SpeechUploadResponse.
func TestUploadSpeechAudio(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/speech/upload" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data;") {
			t.Fatalf("unexpected Content-Type: %s", ct)
		}
		if r.Header.Get("X-Pro-Id") != "p_123" {
			t.Fatalf("expected X-Pro-Id header, got %q", r.Header.Get("X-Pro-Id"))
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm failed: %v", err)
		}
		if got := r.FormValue("proId"); got != "p_123" {
			t.Fatalf("unexpected proId: %q", got)
		}
		if got := r.FormValue("sessionId"); got != "s_1" {
			t.Fatalf("unexpected sessionId: %q", got)
		}
		if got := r.FormValue("chunkIndex"); got != "0" {
			t.Fatalf("unexpected chunkIndex: %q", got)
		}

		// Return a minimal valid JSON body.
		id := int64(42)
		sr := 16000
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SpeechUploadResponse{
			Ok:         true,
			Existed:    false,
			ID:         &id,
			ProID:      "p_123",
			SessionID:  "s_1",
			ChunkIndex: 0,
			SampleRate: &sr,
			StoredPath: "speech_store/p_123/s_1/0.raw",
		})
	}

	client, server := newTestClient(t, handler)
	defer server.Close()

	client.SetAuth("p_123", "tok_abc")

	req := UploadSpeechAudioRequest{
		ProID:      "p_123",
		SessionID:  "s_1",
		ChunkIndex: 0,
		Audio:      strings.NewReader("dummy"),
		FileName:   "test.raw",
		SampleRate: 16000,
	}

	resp, err := client.UploadSpeechAudio(context.Background(), req)
	if err != nil {
		t.Fatalf("UploadSpeechAudio returned error: %v", err)
	}
	if resp.ID == nil || *resp.ID != 42 {
		t.Fatalf("unexpected ID: %#v", resp.ID)
	}
}

// TestUploadSpeechText verifies JSON payload and that the response is
// exposed as raw JSON without schema assumptions.
func TestUploadSpeechText(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/speech/text" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("unexpected Content-Type: %s", ct)
		}

		var body UploadSpeechTextRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		if body.ProID != "p_123" || body.SessionID != "s_1" || body.ChunkIndex != 0 {
			t.Fatalf("unexpected body: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"id":99}`))
	}

	client, server := newTestClient(t, handler)
	defer server.Close()

	resp, err := client.UploadSpeechText(context.Background(), UploadSpeechTextRequest{
		ProID:      "p_123",
		SessionID:  "s_1",
		ChunkIndex: 0,
		Text:       "hello world",
	})
	if err != nil {
		t.Fatalf("UploadSpeechText returned error: %v", err)
	}
	if len(resp.Raw) == 0 {
		t.Fatalf("expected non-empty Raw response")
	}
}

// TestGetSpeechStatusByID validates query construction for id-only lookup.
func TestGetSpeechStatusByID(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/speech/status" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("id") != "123" {
			t.Fatalf("expected id=123, got %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		id := int64(123)
		_ = json.NewEncoder(w).Encode(SpeechStatusResponse{
			Ok:        true,
			Found:     true,
			ID:        &id,
			ProID:     "p_123",
			SessionID: "s_1",
		})
	}

	client, server := newTestClient(t, handler)
	defer server.Close()

	resp, err := client.GetSpeechStatusByID(context.Background(), 123)
	if err != nil {
		t.Fatalf("GetSpeechStatusByID returned error: %v", err)
	}
	if resp.ID == nil || *resp.ID != 123 {
		t.Fatalf("unexpected ID: %#v", resp.ID)
	}
}

// TestGetFactsSnapshot verifies GET /api/facts/items/snapshot behavior.
func TestGetFactsSnapshot(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/facts/items/snapshot" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("proId") != "p_123" {
			t.Fatalf("unexpected proId: %v", q)
		}
		if q.Get("limit") != "10" {
			t.Fatalf("unexpected limit: %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		now := time.Now().UTC()
		resp := FactsItemsResponse{
			ProID:           "p_123",
			CursorUpdatedUTC: now,
			CursorID:        5,
			Items: []FactItem{
				{
					ID:       1,
					ProID:    "p_123",
					FactText: "example",
					FactHash: "hash",
					Status:   "ok",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}

	client, server := newTestClient(t, handler)
	defer server.Close()

	resp, err := client.GetFactsSnapshot(context.Background(), "p_123", 10)
	if err != nil {
		t.Fatalf("GetFactsSnapshot returned error: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].FactText != "example" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

// TestGetFactsUpdates verifies GET /api/facts/items/updates behavior.
func TestGetFactsUpdates(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/facts/items/updates" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("proId") != "p_123" {
			t.Fatalf("unexpected proId: %v", q)
		}
		if q.Get("sinceId") != "5" {
			t.Fatalf("unexpected sinceId: %v", q)
		}
		if _, err := time.Parse(time.RFC3339, q.Get("sinceUpdatedUtc")); err != nil {
			t.Fatalf("invalid sinceUpdatedUtc: %v", q.Get("sinceUpdatedUtc"))
		}
		w.Header().Set("Content-Type", "application/json")
		now := time.Now().UTC()
		resp := FactsUpdatesResponse{
			ProID:           "p_123",
			CursorUpdatedUTC: now,
			CursorID:        6,
			Items: []FactItem{
				{
					ID:       6,
					ProID:    "p_123",
					FactText: "updated",
					FactHash: "hash2",
					Status:   "ok",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}

	client, server := newTestClient(t, handler)
	defer server.Close()

	resp, err := client.GetFactsUpdates(
		context.Background(),
		"p_123",
		time.Now(),
		5,
		50,
	)
	if err != nil {
		t.Fatalf("GetFactsUpdates returned error: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].ID != 6 {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

// TestGetMatchesSnapshot verifies GET /api/matches/items/snapshot behavior.
func TestGetMatchesSnapshot(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/matches/items/snapshot" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("proId") != "p_123" || q.Get("direction") != "Offer" {
			t.Fatalf("unexpected query: %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		now := time.Now().UTC()
		resp := MatchesItemsResponse{
			ProID:           "p_123",
			Direction:       MatchingDirectionOffer,
			CursorUpdatedUTC: now,
			CursorID:        10,
			Items: []MatchItem{
				{
					ID:          1,
					ProID:       "p_123",
					TargetProID: "p_456",
					Direction:   MatchingDirectionOffer,
					Score:       0.9,
					Rationale:   "strong match",
					ModelID:     "m1",
					CreatedUTC:  now,
					UpdatedUTC:  now,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}

	client, server := newTestClient(t, handler)
	defer server.Close()

	resp, err := client.GetMatchesSnapshot(
		context.Background(),
		"p_123",
		MatchingDirectionOffer,
		0.0,
		100,
		0,
		0,
	)
	if err != nil {
		t.Fatalf("GetMatchesSnapshot returned error: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].TargetProID != "p_456" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

// TestGetMatchesUpdates verifies GET /api/matches/items/updates behavior.
func TestGetMatchesUpdates(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/matches/items/updates" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("proId") != "p_123" {
			t.Fatalf("unexpected proId: %v", q)
		}
		if q.Get("direction") != "Offer" {
			t.Fatalf("unexpected direction: %v", q)
		}
		if q.Get("sinceId") != "10" {
			t.Fatalf("unexpected sinceId: %v", q)
		}
		if _, err := time.Parse(time.RFC3339, q.Get("sinceUpdatedUtc")); err != nil {
			t.Fatalf("invalid sinceUpdatedUtc: %v", q.Get("sinceUpdatedUtc"))
		}
		w.Header().Set("Content-Type", "application/json")
		now := time.Now().UTC()
		dir := MatchingDirectionOffer
		resp := MatchesUpdatesResponse{
			ProID:           "p_123",
			Direction:       &dir,
			CursorUpdatedUTC: now,
			CursorID:        11,
			Items: []MatchItem{
				{
					ID:          11,
					ProID:       "p_123",
					TargetProID: "p_789",
					Direction:   MatchingDirectionOffer,
					Score:       0.8,
					Rationale:   "updated match",
					ModelID:     "m1",
					CreatedUTC:  now,
					UpdatedUTC:  now,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}

	client, server := newTestClient(t, handler)
	defer server.Close()

	resp, err := client.GetMatchesUpdates(
		context.Background(),
		"p_123",
		MatchingDirectionOffer,
		time.Now(),
		10,
		0.0,
		100,
		0,
		0,
	)
	if err != nil {
		t.Fatalf("GetMatchesUpdates returned error: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].TargetProID != "p_789" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

// TestAPIError verifies that non-2xx responses produce an *APIError
// with parsed error message when possible.
func TestAPIError(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"error":"bad request"}`))
	}

	client, server := newTestClient(t, handler)
	defer server.Close()

	// Use any method that ends up calling doJSON with non-2xx.
	reqURL, _ := url.Parse(server.URL + "/api/speech/status")
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, reqURL.String(), nil)
	err := client.doJSON(req, nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != http.StatusBadRequest || apiErr.Message != "bad request" {
		t.Fatalf("unexpected APIError: %#v", apiErr)
	}
}
