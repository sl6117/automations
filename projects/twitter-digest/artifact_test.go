package twitterdigest

import (
	"encoding/json"
	"testing"
	"time"
)

// Stored artifacts predate the adapter's structured fields, and the deepdive, rejudge
// and digest-mcp all read them back. They must keep unmarshalling with Kept intact.
func TestOldArtifactWithoutStructuredFieldsStillUnmarshals(t *testing.T) {
	old := `{
 "ts": "2026-07-29T16-00-05Z",
 "model": "claude-haiku-4-5",
 "kept": [
  {
   "ID": "1900000000000000001",
   "Author": "Example Wire",
   "Handle": "@example_wire",
   "Text": "RT @example_desk: $EXMPL beat estimates",
   "URL": "https://x.com/example_wire/status/1900000000000000001",
   "Likes": 0,
   "Reposts": 40,
   "Replies": 1,
   "Quotes": 2,
   "Bookmarks": 3,
   "Impressions": 900,
   "CreatedAt": "2026-07-29T15:00:00Z",
   "AuthorFollowers": 1200
  }
 ],
 "language": "English",
 "digest": "## Stocks\n- something (@example_wire https://x.com/example_wire/status/1900000000000000001)",
 "inputTokens": 100,
 "outputTokens": 20,
 "evalFailures": null,
 "evalCoverage": "1/1"
}`
	var a Artifact
	if err := json.Unmarshal([]byte(old), &a); err != nil {
		t.Fatalf("old artifact no longer unmarshals: %v", err)
	}
	if len(a.Kept) != 1 {
		t.Fatalf("Kept length = %d, want 1", len(a.Kept))
	}
	k := a.Kept[0]
	if k.ID != "1900000000000000001" || k.Handle != "@example_wire" || k.Text != "RT @example_desk: $EXMPL beat estimates" ||
		k.URL != "https://x.com/example_wire/status/1900000000000000001" || k.Reposts != 40 || k.AuthorFollowers != 1200 ||
		!k.CreatedAt.Equal(time.Date(2026, 7, 29, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("Kept[0] lost existing fields: %+v", k)
	}
	if k.AuthorID != "" || k.OwnText != "" || k.Refs != nil {
		t.Errorf("structured fields absent from the blob must stay zero, got %+v", k)
	}
}
