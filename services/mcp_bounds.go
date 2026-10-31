package services

// Keep a single large changed line from making the entire preview unreadable.
// The retained plan always keeps complete content; only tool output is bounded.
func mcpDiff(lines []*BatchDiffLine) ([]*BatchDiffLine, bool) {
	out := []*BatchDiffLine{}
	budget := 16 << 10
	truncated := false
	for _, line := range lines {
		if budget <= 0 || len(out) >= 100 {
			truncated = true
			break
		}
		copy := *line
		if len(copy.Text) > 1024 {
			copy.Text = copy.Text[:1024] + "…"
			truncated = true
		}
		budget -= len(copy.Text)
		out = append(out, &copy)
	}
	return out, truncated
}
func boundedScriptPage(page *ScriptPreviewPage) *ScriptPreviewPage {
	copy := *page
	copy.Rows = nil
	for _, row := range page.Rows {
		r := *row
		var cut bool
		r.Diff, cut = mcpDiff(row.Diff)
		r.DiffTruncated = r.DiffTruncated || cut
		copy.Rows = append(copy.Rows, &r)
	}
	return &copy
}
