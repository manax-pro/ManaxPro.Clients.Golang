# manax-go — Go client for Manax ApiService

`manax-go` is a production‑oriented Go client library for the **Manax Pro** backend (ApiService).

It provides:

- Typed HTTP helpers for all REST endpoints:
  - Crypto / Pro wallet.
  - Speech ingestion (audio + text) and ASR status.
  - Facts snapshot and incremental updates.
  - Matches snapshot and incremental updates.
- Robust SSE streaming utilities:
  - Streaming facts updates.
  - Streaming matches updates.
- Environment‑driven configuration (`MANAX_*` variables).
- CI‑friendly layout (Go modules, Dockerfile, GitHub Actions).

This document describes how to use the library in real‑world scenarios.

---

## 1. Installation

```bash
go get github.com/manax-pro/manax-go
```

---

## 2. Configuration

The library can be configured in two ways:

1. **Imperatively** — by explicitly passing the base URL and HTTP client.
2. **From environment variables** — via `NewClientFromEnv`.

### 2.1 Environment variables

`NewClientFromEnv` reads the following variables:

- **MANAX_BASE_URL** *(required)* — Base URL of ApiService.  
  Examples: `https://api.manax.pro`, `http://localhost:5010`
- **MANAX_KEY** — Optional privileged API key for wallet creation.
- **MANAX_STORAGE_KEY** — Optional storage key for blob operations.
- **MANAX_PRO_ID / MANAX_PRO_TOKEN** — Logical profile identity.
- **MANAX_HTTP_TIMEOUT_SEC** — HTTP timeout in seconds.
- **MANAX_SSE_IDLE_TIMEOUT_SEC** — Recommended SSE idle timeout.
- **MANAX_FACTS_SNAPSHOT_LIMIT** — Default limit for facts snapshot.
- **MANAX_MATCHES_SNAPSHOT_LIMIT** — Default limit for matches snapshot.
- **MANAX_MATCHES_MIN_SCORE** — Default `minScore` for matches.
- **MANAX_MATCHES_DIRECTION** — Default direction (`Offer` or `Seek`).
- **MANAX_HTTP_DEBUG** — Enable verbose HTTP logging (`1`, `true`, `yes`).
- **MANAX_USER_AGENT** — Custom `User-Agent` header.

#### Example `.env` (never commit real secrets)

```env
MANAX_BASE_URL=https://api.manax.pro
MANAX_KEY=your-admin-key
MANAX_PRO_ID=pro_123
MANAX_PRO_TOKEN=secret_token
MANAX_HTTP_TIMEOUT_SEC=20
MANAX_MATCHES_DIRECTION=Offer
MANAX_MATCHES_MIN_SCORE=0.55
```

---

## 3. Creating a client

### 3.1 From environment (recommended)

```go
import (
    "log"

    "github.com/manax-labs/manax-go/manax"
)

func mustClientFromEnv() *manax.Client {
    client, err := manax.NewClientFromEnv()
    if err != nil {
        log.Fatalf("failed to create client: %v", err)
    }
    return client
}
```

This is the recommended entry point for CLI tools and examples.

### 3.2 Explicit configuration

```go
import (
    "net/http"
    "time"

    "github.com/manax-labs/manax-go/manax"
)

func manualClient() *manax.Client {
    httpClient := &http.Client{
        Timeout: 30 * time.Second,
    }

    client, err := manax.NewClient(
        "https://api.manax.pro",
        manax.WithHTTPClient(httpClient),
        manax.WithDefaultHeader("User-Agent", "my-app/1.0"),
    )
    if err != nil {
        panic(err)
    }

    client.SetProfileAuth("pro_123", "secret_token")
    return client
}
```

---

## 4. Crypto — Pro wallet operations

### 4.1 Creating a new pro wallet

**Endpoint:**

```
POST /api/crypto/pro-wallet/create
```

**Headers:** optional `X-Manax-Key`

```go
client := mustClientFromEnv()

resp, err := client.CreateProWallet(context.Background())
if err != nil {
    log.Fatalf("CreateProWallet failed: %v", err)
}

log.Printf("new pro wallet: id=%s token=%s", resp.ProID, resp.Token)
```

Persist `proId` and `token` securely on the client side.

### 4.2 Verifying a pro wallet token

**Endpoint:**

```
GET /api/crypto/pro-wallet/verify?proId=...&token=...
```

```go
okResp, err := client.VerifyProWallet(ctx, "pro_123", "secret_token")
if err != nil {
    log.Fatalf("VerifyProWallet failed: %v", err)
}

if !okResp.Valid {
    log.Printf("token is invalid or expired for proId=%s", okResp.ProID)
}
```

---

## 5. Speech ingest and ASR status

### 5.1 Uploading audio

**Endpoint:**

```
POST /api/speech/upload
```

```go
resp, err := client.UploadSpeechAudio(ctx, manax.UploadSpeechAudioParams{
    ProID:       "pro_123",
    SessionID:   "sess_001",
    ChunkIndex:  0,
    Bytes:       audioBytes,
    FileName:    "chunk001.wav",
    ContentType: "audio/wav",
    SampleRate:  16000,
})
if err != nil {
    log.Fatalf("UploadSpeechAudio failed: %v", err)
}
```

### 5.2 Uploading text transcript

```go
err := client.UploadSpeechText(ctx, manax.UploadSpeechTextParams{
    ProID:      "pro_123",
    SessionID:  "sess_001",
    ChunkIndex: 0,
    Text:       "I would like to find collaborators for a social impact startup...",
})
```

### 5.3 Polling ASR status

```go
status, err := client.GetSpeechStatusByID(ctx, 123)
if err != nil {
    log.Fatalf("GetSpeechStatusByID failed: %v", err)
}

log.Printf("ASR status=%s transcript=%q", status.ASRStatus, status.Transcript)
```

---

## 6. Facts snapshot and updates (HTTP)

### 6.1 Snapshot

```go
snap, err := client.GetFactsSnapshot(ctx, "pro_123", &manax.FactsSnapshotOptions{
    Limit: 500,
})
```

### 6.2 Incremental updates

```go
updates, err := client.GetFactsUpdates(ctx, manax.FactsUpdatesParams{
    ProID:          "pro_123",
    SinceUpdatedUTC: snap.CursorUpdatedUTC,
    SinceID:        snap.CursorID,
    Limit:          500,
})
```

---

## 7. Facts streaming (SSE)

```go
factsStream := manax.NewFactsStreamClient(client, nil)

err := factsStream.StreamFacts(ctx, "pro_123", func(ctx context.Context, chunk *manax.FactsStreamChunk) error {
    for _, f := range chunk.Items {
        log.Printf("[%d] %s", f.ID, f.FactText)
    }
    return nil
})
```

---

## 8. Matches snapshot and updates (HTTP)

### 8.1 Snapshot

```go
snap, err := client.GetMatchesSnapshot(ctx, manax.MatchesSnapshotParams{
    ProID:     "pro_123",
    Direction: manax.MatchingDirectionOffer,
    MinScore:  0.5,
    Limit:     200,
})
```

### 8.2 Updates

```go
updates, err := client.GetMatchesUpdates(ctx, manax.MatchesUpdatesParams{
    ProID:          "pro_123",
    Direction:      manax.MatchingDirectionOffer,
    SinceUpdatedUTC: snap.CursorUpdatedUTC,
    SinceID:        snap.CursorID,
    MinScore:       0.5,
    Limit:          200,
})
```

---

## 9. Matches streaming (SSE)

```go
stream := manax.NewMatchesStreamClient(client, nil)

err := stream.StreamMatches(ctx, opts, func(ctx context.Context, chunk *manax.MatchesStreamChunk) error {
    for _, m := range chunk.Items {
        log.Printf("live match -> %s (score=%.3f)", m.TargetProID, m.Score)
    }
    return nil
})
```

---

## 10. Error handling

```go
resp, err := client.GetFactsSnapshot(ctx, "pro_123", nil)
if err != nil {
    var apiErr *manax.APIError
    if errors.As(err, &apiErr) {
        log.Printf("API error: status=%d message=%s", apiErr.StatusCode, apiErr.Message)
    } else {
        log.Printf("transport error: %v", err)
    }
}
```

---

## 11. Running tests and linting

```bash
go test ./...
golangci-lint run
```

With Docker:

```bash
docker build -t manax-go .
docker run --rm manax-go
```

---

## 12. Versioning and stability

The library follows semantic versioning:

- Backwards‑compatible additions → **minor** version bump.
- Breaking changes → **major** version bump.

Always pin an exact version tag in `go.mod` for production use.
