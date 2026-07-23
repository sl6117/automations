package weeklydeepdive

import (
	"fmt"
	"strings"
)

func renderBrief(b Brief, ed EditorReport) string {
	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(b.Title)
	sb.WriteString("\n\n")
	sb.WriteString(b.Summary)
	sb.WriteString("\n")
	for _, s := range b.Sections {
		sb.WriteString("\n## ")
		sb.WriteString(s.Heading)
		sb.WriteString("\n\n")
		sb.WriteString(s.Body)
		sb.WriteString("\n")
	}
	sb.WriteString("\n---\n")
	if ed.Pass {
		sb.WriteString(fmt.Sprintf("_Editor: pass (contract check)._\n"))
	} else {
		sb.WriteString(fmt.Sprintf("_Editor: fail — %s_\n", strings.Join(ed.Failures, "; ")))
	}
	return sb.String()
}
