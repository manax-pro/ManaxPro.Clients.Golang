package manaxclient

import (
	"encoding/json"
	"time"
)

// CreateProWalletResponse models the JSON body returned by
// POST /api/crypto/pro-wallet/create.
//
// This structure is based on your previous client contract:
//   {
//     "proId":      "...",
//     "token":      "...",
//     "mnemonic24": "...",
//     "createdUtc": "2025-01-01T00:00:00Z"
//   }
//
// Mnemonic24 is represented as a single string containing 24 space-separated
// words; the exact formatting is determined by the server.
type CreateProWalletResponse struct {
	// ProID is the newly created profile identifier.
	ProID string `json:"proId"`

	// Token is the authentication token that should be persisted securely
	// on the client side and later sent via X-Pro-Token header.
	Token string `json:"token"`

	// Mnemonic24 is a 24-word mnemonic phrase encoded as a single string.
	Mnemonic24 string `json:"mnemonic24"`

	// CreatedUTC is the UTC timestamp when the wallet was created.
	CreatedUTC time.Time `json:"createdUtc"`
}

// VerifyProWalletResponse models the JSON body returned by
// GET /api/crypto/pro-wallet/verify.
//
// Typical shape:
//   {
//     "proId": "...",
//     "valid": true
//   }
type VerifyProWalletResponse struct {
	// ProID is the profile identifier whose token was validated.
	ProID string `json:"proId"`

	// Valid indicates whether the provided token is accepted by the server.
	Valid bool `json:"valid"`
}

// UploadSpeechAudioRequest describes the input required to upload
// an audio chunk to /api/speech/upload.
type UploadSpeechAudioRequest struct {
	// ProID is the logical profile id (optional from server perspective,
	// but recommended). If not specified on the server, "p_anon" is often used.
	ProID string

	// SessionID groups multiple chunks belonging to the same logical
	// recording or conversation.
	SessionID string

	// ChunkIndex is the 0-based index of the chunk within the session.
	ChunkIndex int

	// Audio is an io.Reader providing the binary audio content.
	Audio io.Reader

	// FileName is an optional filename used in multipart request.
	// If empty, "audio" will be used.
	FileName string

	// SampleRate is the sampling rate in Hz; if 0, it is omitted and the
	// server may auto-detect or use a default.
	SampleRate int
}

// SpeechUploadResponse mirrors the C# SpeechUploadResponse model in
// Manax.ProServer.ApiService, and represents the server response to
// POST /api/speech/upload.
type SpeechUploadResponse struct {
	// Ok indicates whether the upload request was accepted successfully.
	Ok bool `json:"ok"`

	// Existed is true if this exact chunk already existed in storage.
	Existed bool `json:"existed"`

	// ID is the optional numeric identifier assigned to the uploaded row.
	// It may be nil if the server chooses not to return it.
	ID *int64 `json:"id"`

	// ProID is the profile identifier associated with this chunk.
	ProID string `json:"proId"`

	// SessionID is the session identifier for this chunk.
	SessionID string `json:"sessionId"`

	// ChunkIndex is the index of this chunk within its session.
	ChunkIndex int `json:"chunkIndex"`

	// SampleRate is the effective sample rate in Hz, if the server was
	// able to determine it.
	SampleRate *int `json:"sampleRate"`

	// StoredPath is the internal storage path for the original audio file.
	StoredPath string `json:"storedPath"`

	// Wav16kMonoPath is the optional path to the normalized 16kHz mono WAV
	// file used for ASR.
	Wav16kMonoPath *string `json:"wav16kMonoPath"`

	// Transcript contains the recognized transcript if ASR has already
	// been performed; otherwise it may be empty.
	Transcript string `json:"transcript"`
}

// UploadSpeechTextRequest represents the JSON payload sent to
// POST /api/speech/text to attach raw text to a given speech chunk.
type UploadSpeechTextRequest struct {
	// ProID is the profile identifier; may be empty if server allows.
	ProID string `json:"proId"`

	// SessionID identifies the logical recording session.
	SessionID string `json:"sessionId"`

	// ChunkIndex is the index of the chunk to which the text belongs.
	ChunkIndex int `json:"chunkIndex"`

	// Text is the raw textual content to attach.
	Text string `json:"text"`
}

// UploadSpeechTextResponse wraps the opaque JSON returned by
// POST /api/speech/text. The library intentionally does not fix a schema
// here because the actual fields depend on server implementation.
//
// Callers may unmarshal Raw into their own structures or work with it
// as generic JSON.
type UploadSpeechTextResponse struct {
	// Raw holds the entire JSON response as returned by the server.
	Raw json.RawMessage
}

// SpeechStatusResponse models the JSON body returned by
// GET /api/speech/status. It mirrors the C# SpeechStatusResponse type.
type SpeechStatusResponse struct {
	// Ok is true if the lookup itself was successful.
	Ok bool `json:"ok"`

	// Found indicates whether a row with the requested key/id exists.
	Found bool `json:"found"`

	// ID is the numeric id of the row, if found.
	ID *int64 `json:"id"`

	// ProID is the profile id associated with the row.
	ProID string `json:"proId"`

	// SessionID is the session id of the row.
	SessionID string `json:"sessionId"`

	// ChunkIndex is the chunk index within the session.
	ChunkIndex int `json:"chunkIndex"`

	// AsrStatus describes the current ASR status, such as:
	//   - "pending"
	//   - "ok"
	//   - "error"
	AsrStatus string `json:"asrStatus"`

	// AsrError optionally holds a textual description of any ASR error.
	AsrError *string `json:"asrError"`

	// Transcript is the recognized text if available.
	Transcript string `json:"transcript"`

	// DurationSec is the duration of the audio segment in seconds, if known.
	DurationSec *float64 `json:"durationSec"`

	// AudioSha256 is the SHA-256 hash of the stored audio, if computed.
	AudioSha256 *string `json:"audioSha256"`
}

// FactItem models a single fact row as exposed via FactsEngine's
// FactItemDto (id, text, hashes, review status, timestamps, etc.).
type FactItem struct {
	ID              int64      `json:"id"`
	ProID           string     `json:"proId"`
	FactText        string     `json:"factText"`
	FactHash        string     `json:"factHash"`
	Status          string     `json:"status"`          // "ok" | "stale" | "false"
	FalseReason     *string    `json:"falseReason"`     // explanation if status == "false"
	CreatedUTC      time.Time  `json:"createdUtc"`
	LastSeenUTC     time.Time  `json:"lastSeenUtc"`
	UpdatedUTC      time.Time  `json:"updatedUtc"`
	ReviewStatus    *string    `json:"reviewStatus"`    // "ok" | "not" | null
	ReviewUpdatedUTC *time.Time `json:"reviewUpdatedUtc"`
	IsWritable      bool       `json:"isWritable"`
}

// FactsItemsResponse represents the response of
// GET /api/facts/items/snapshot for a specific proId.
//
// It contains a cursor (cursorUpdatedUtc, cursorId) and a set of facts
// that can be used as a baseline for subsequent incremental updates.
type FactsItemsResponse struct {
	// ProID is the profile identifier to which the facts belong.
	ProID string `json:"proId"`

	// CursorUpdatedUTC is the maximum UpdatedUtc across all items in this
	// snapshot. It is used as part of the incremental cursor.
	CursorUpdatedUTC time.Time `json:"cursorUpdatedUtc"`

	// CursorID is the id of the last item in the snapshot when ordered
	// by (UpdatedUtc, Id). Together with CursorUpdatedUTC it forms a
	// stable pagination cursor.
	CursorID int64 `json:"cursorId"`

	// Items is the list of fact items included in this snapshot.
	Items []FactItem `json:"items"`
}

// FactsUpdatesResponse represents the response of
// GET /api/facts/items/updates.
//
// It has the same cursor structure as FactsItemsResponse but only
// includes items that changed since the previous cursor.
type FactsUpdatesResponse struct {
	// ProID is the profile identifier to which the updates apply.
	ProID string `json:"proId"`

	// CursorUpdatedUTC is the updated cursor timestamp after applying
	// the updates from this response.
	CursorUpdatedUTC time.Time `json:"cursorUpdatedUtc"`

	// CursorID is the updated cursor id after applying the updates.
	CursorID int64 `json:"cursorId"`

	// Items contains the updated fact items since the last cursor.
	Items []FactItem `json:"items"`
}

// PatchReviewStatusRequest models the JSON payload sent to
// PATCH /api/facts/items/{id}/review-status.
//
// ReviewStatus is usually one of:
//   - "ok"   : fact explicitly confirmed.
//   - "not"  : fact explicitly rejected.
//   - ""     : to clear the review status (server may interpret as null).
type PatchReviewStatusRequest struct {
	ReviewStatus string `json:"reviewStatus"`
}

// PatchReviewStatusResponse mirrors the C# PatchReviewStatusResponse
// type on the ApiService side (code, reason).
//
// Code is typically:
//   - "ok"
//   - "bad_request"
//   - another error code defined by server.
type PatchReviewStatusResponse struct {
	// Code is a short machine-friendly result code.
	Code string `json:"code"`

	// Reason optionally contains a human-readable explanation.
	Reason *string `json:"reason"`
}

// MatchingDirection represents the direction of matching between profiles.
//
// It is mapped directly to the MatchingDirection enum in C# API, where:
//
//   Offer: "Who needs me?" – the source profile is the provider/helper.
//   Seek : "Who do I need?" – the source profile is looking for help.
//
// The HTTP API accepts these values as query parameters, typically in
// the same PascalCase spelling.
type MatchingDirection string

const (
	// MatchingDirectionOffer means "Who needs me?" – matches where the
	// source profile is offering help, services or collaboration.
	MatchingDirectionOffer MatchingDirection = "Offer"

	// MatchingDirectionSeek means "Who do I need?" – matches where the
	// source profile is seeking helpers or collaborators.
	MatchingDirectionSeek MatchingDirection = "Seek"
)

// MatchItem models a single match row as returned by the matching engine.
//
// It mirrors MatchItemDto in the FactsEngine, including:
//   - directional flags
//   - score
//   - textual rationale
//   - model id
//   - timestamps.
type MatchItem struct {
	// ID is the unique numeric identifier of the match row.
	ID int64 `json:"id"`

	// ProID is the source profile for which this match was computed.
	ProID string `json:"proId"`

	// TargetProID is the profile id of the matched "other" side.
	TargetProID string `json:"targetProId"`

	// Direction indicates how ProID and TargetProID are related.
	Direction MatchingDirection `json:"direction"`

	// Score is the numeric score of the match, typically in [0.0, 1.0].
	Score float64 `json:"score"`

	// Rationale is a textual explanation of why this match was proposed.
	Rationale string `json:"rationale"`

	// ModelID identifies the model or algorithm that produced this match.
	ModelID string `json:"modelId"`

	// CreatedUTC is the timestamp when the match row was created.
	CreatedUTC time.Time `json:"createdUtc"`

	// UpdatedUTC is the timestamp when the match row was last updated.
	UpdatedUTC time.Time `json:"updatedUtc"`
}

// MatchesItemsResponse represents the response body of
// GET /api/matches/items/snapshot.
type MatchesItemsResponse struct {
	// ProID is the source profile id for which matches were computed.
	ProID string `json:"proId"`

	// Direction is the match direction used for this snapshot.
	Direction MatchingDirection `json:"direction"`

	// CursorUpdatedUTC is the cursor timestamp for incremental polling.
	CursorUpdatedUTC time.Time `json:"cursorUpdatedUtc"`

	// CursorID is the cursor id associated with this snapshot.
	CursorID int64 `json:"cursorId"`

	// Items is the list of match items included in the snapshot.
	Items []MatchItem `json:"items"`
}

// MatchesUpdatesResponse represents the response body of
// GET /api/matches/items/updates.
type MatchesUpdatesResponse struct {
	// ProID is the profile id for which updates are returned.
	ProID string `json:"proId"`

	// Direction is the direction of matches included in the update set.
	// It may be null in C# (both directions), therefore it is modeled
	// as a pointer here; nil means "not specified".
	Direction *MatchingDirection `json:"direction"`

	// CursorUpdatedUTC is the updated cursor timestamp after applying
	// the updates from this response.
	CursorUpdatedUTC time.Time `json:"cursorUpdatedUtc"`

	// CursorID is the updated cursor id.
	CursorID int64 `json:"cursorId"`

	// Items contains incremental match items since the last cursor.
	Items []MatchItem `json:"items"`
}
