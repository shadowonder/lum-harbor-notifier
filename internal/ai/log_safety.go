package ai

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	authorizationPattern = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)([^,;\r\n]+)`)
	bearerPattern        = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`)
	secretValuePattern   = regexp.MustCompile(`(?i)((?:"?(?:api[_ -]?key|access[_ -]?token|refresh[_ -]?token|session[_ -]?token|token|password|passwd|secret|credential|client[_ -]?secret|private[_ -]?key)"?)\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^,;\s}\]]+)`)
	openAIKeyPattern     = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`)
	privateKeyPattern    = regexp.MustCompile(`(?s)-----BEGIN(?: [A-Z0-9]+)? PRIVATE KEY-----.*?-----END(?: [A-Z0-9]+)? PRIVATE KEY-----`)
	jwtPattern           = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
)

// SafeLogText defensively redacts common secret forms, flattens whitespace,
// and rune-bounds AI-owned free text before it reaches slog.
func SafeLogText(value string, maxRunes int) string {
	value = authorizationPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = bearerPattern.ReplaceAllString(value, `Bearer [REDACTED]`)
	value = secretValuePattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = openAIKeyPattern.ReplaceAllString(value, `[REDACTED]`)
	value = privateKeyPattern.ReplaceAllString(value, `[REDACTED PRIVATE KEY]`)
	value = jwtPattern.ReplaceAllString(value, `[REDACTED]`)
	value = strings.Join(strings.Fields(value), " ")
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	remaining := len(runes) - maxRunes
	return string(runes[:maxRunes]) + fmt.Sprintf("…(+%d chars)", remaining)
}
