package handler

import (
	"encoding/json"
	"strings"
	"time"
)

// tryParse parses `value` against `layout` and returns (unix-seconds, true) on
// success. Trims surrounding whitespace first.
func tryParse(layout, value string) (int64, bool) {
	t, err := time.Parse(layout, strings.TrimSpace(value))
	if err != nil {
		return 0, false
	}
	return t.Unix(), true
}

// unmarshalLooseJSON tries to decode a single JSON object into dst.
// Returns false if the line doesn't start with '{' or fails to parse.
func unmarshalLooseJSON(line string, dst any) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") {
		return false
	}
	return json.Unmarshal([]byte(line), dst) == nil
}

// shellQuote wraps a value in single quotes for safe inclusion in a shell
// command. Embedded single quotes are escaped with the standard '\'' trick.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// stripHTMLTags does a crude strip of HTML tags. Used for parsing
// Unraid PHP endpoint responses that return HTML instead of JSON.
func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, c := range s {
		if c == '<' {
			inTag = true
			continue
		}
		if c == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(c)
		}
	}
	return b.String()
}
