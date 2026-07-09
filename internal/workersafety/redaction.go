package workersafety

import (
	"regexp"
	"strings"
	"unicode"
)

const Redacted = "[REDACTED]"

var secretTextPatterns = []struct {
	re      *regexp.Regexp
	replace string
}{
	{regexp.MustCompile(`(?i)\bBearer[ \t]+[A-Za-z0-9._~+/=-]{8,}`), "Bearer " + Redacted},
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`), Redacted},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`), Redacted},
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), Redacted},
	{regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:TOKEN|PASSWORD|PASSWD|SECRET|API_KEY|ACCESS_KEY|AUTHORIZATION)[A-Z0-9_]*)=([^\s,;]+)`), `${1}=` + Redacted},
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`), Redacted},
}

var protectedStructuralKeys = map[string]struct{}{
	"kind": {}, "status": {}, "runid": {}, "instructionid": {}, "target": {},
	"operation": {}, "native_session_id": {}, "nativesessionid": {},
	"final_path": {}, "finalpath": {}, "created_at": {}, "createdat": {},
}

var secretWords = map[string]struct{}{
	"token": {}, "password": {}, "passwd": {}, "secret": {}, "authorization": {},
	"cookie": {}, "credential": {}, "credentials": {},
}

// RedactRecord returns a deep-copied record whose obvious secret-bearing fields
// and values are masked. It does not mutate the input record.
func RedactRecord(input map[string]any) (map[string]any, bool) {
	if input == nil {
		return nil, false
	}
	value, changed := redactValue(input, "")
	return value.(map[string]any), changed
}

// RedactValue redacts a JSON-compatible value while preserving its structure.
func RedactValue(input any) (any, bool) {
	return redactValue(input, "")
}

func redactValue(input any, key string) (any, bool) {
	if key != "" && isSecretKey(key) {
		return Redacted, true
	}

	switch value := input.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		changed := false
		for childKey, childValue := range value {
			redacted, childChanged := redactValue(childValue, childKey)
			out[childKey] = redacted
			changed = changed || childChanged
		}
		return out, changed
	case map[string]string:
		out := make(map[string]string, len(value))
		changed := false
		for childKey, childValue := range value {
			redacted, childChanged := redactValue(childValue, childKey)
			out[childKey] = redacted.(string)
			changed = changed || childChanged
		}
		return out, changed
	case []any:
		out := make([]any, len(value))
		changed := false
		for index, childValue := range value {
			redacted, childChanged := redactValue(childValue, "")
			out[index] = redacted
			changed = changed || childChanged
		}
		return out, changed
	case string:
		redacted := value
		for _, pattern := range secretTextPatterns {
			redacted = pattern.re.ReplaceAllString(redacted, pattern.replace)
		}
		return redacted, redacted != value
	default:
		return input, false
	}
}

func isSecretKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	compact := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, normalized)
	if _, ok := protectedStructuralKeys[normalized]; ok {
		return false
	}
	if _, ok := protectedStructuralKeys[compact]; ok {
		return false
	}

	words := splitKeyWords(key)
	for _, word := range words {
		if _, ok := secretWords[word]; ok {
			return true
		}
	}
	pairs := strings.Join(words, "")
	for _, compound := range []string{"apikey", "accesskey", "privatekey", "clientsecret", "signingkey"} {
		if strings.Contains(pairs, compound) {
			return true
		}
	}
	return false
}

func splitKeyWords(key string) []string {
	var words []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			words = append(words, strings.ToLower(string(current)))
			current = nil
		}
	}
	var previousLower bool
	for _, r := range key {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			previousLower = false
			continue
		}
		if unicode.IsUpper(r) && previousLower {
			flush()
		}
		current = append(current, unicode.ToLower(r))
		previousLower = unicode.IsLower(r) || unicode.IsDigit(r)
	}
	flush()
	return words
}
