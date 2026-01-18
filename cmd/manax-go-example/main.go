package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/manax-pro/manax-go/manaxclient"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	client, err := manax.NewClientFromEnv()
	if err != nil {
		log.Fatalf("failed to create client from env: %v", err)
	}

	proID := os.Getenv("MANAX_PRO_ID")
	if proID == "" {
		log.Fatal("MANAX_PRO_ID must be set for the example")
	}

	// Example: fetch facts snapshot.
	snapshot, err := client.GetFactsSnapshot(context.Background(), proID, nil)
	if err != nil {
		log.Fatalf("GetFactsSnapshot failed: %v", err)
	}
	log.Printf("facts snapshot: %d items (cursorId=%d)", len(snapshot.Items), snapshot.CursorID)

	// Example: stream matches until interrupted.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cursor := manax.MatchesStreamCursor{
		UpdatedUTC: snapshot.CursorUpdatedUTC,
		ID:         int64(snapshot.CursorID),
	}
	opts := manax.MatchesStreamOptions{
		ProID:     proID,
		Direction: manax.MatchingDirectionOffer,
		Cursor:    cursor,
	}

	matchesStream := manax.NewMatchesStreamClient(client, nil)

	log.Println("starting matches SSE stream; press Ctrl+C to stop")

	err = matchesStream.StreamMatches(ctx, opts, func(ctx context.Context, chunk *manax.MatchesStreamChunk) error {
		for _, m := range chunk.Items {
			fmt.Printf("[%s] match #%d target=%s score=%.3f\n",
				m.UpdatedUTC.Format(time.RFC3339),
				m.ID,
				m.TargetProID,
				m.Score,
			)
		}
		return nil
	})
	if err != nil && err != context.Canceled {
		log.Fatalf("StreamMatches failed: %v", err)
	}

	log.Println("stream finished")
}
