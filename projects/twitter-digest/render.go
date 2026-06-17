package twitterdigest

import (
	"fmt"
	"strings"
	"time"
)

// render turns the structured Digeest into human-readable text
// formatting lives in the project (not sink), so the sink stays generic
func render(digest Digest) string {
	var buf strings.Builder

	buf.WriteString("Daily X Digest \n=======\n")
	buf.WriteString(time.Now().Format("2006-01-02"))

	for _, bucket := range digest.Buckets {
		fmt.Fprintf(&buf, "\n## %s\n", bucket.Topic)

		for _, t := range bucket.Tweets {
			fmt.Fprintf(&buf, "- %s (%s): %s [%s]\n", t.Author, t.Handle, t.Text, t.URL)
		}
	}

	return buf.String()
}
