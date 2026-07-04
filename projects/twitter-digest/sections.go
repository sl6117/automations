package twitterdigest

import "strings"

// Section is one "## Topic" block of a rendered digest, recovered by parsing the model's output.
// The same format the eval validates.
type Section struct {
	Topic string
	Body  string
}

// splitSections parses digest text into topic sections. Text before the first "## " header (e.g. the heuristic renderer's title line) is discarded.
// the router re-adds its own header per subscriber
func splitSections(digest string) []Section {
	var sections []Section
	for _, line := range strings.Split(digest, "\n") {
		if strings.HasPrefix(line, "## ") {
			topic := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			sections = append(sections, Section{Topic: topic})
			continue
		}
		if len(sections) > 0 {
			sections[len(sections)-1].Body += line + "\n"
		}
	}
	for i := range sections {
		sections[i].Body = strings.TrimSpace(sections[i].Body)
	}
	return sections
}
