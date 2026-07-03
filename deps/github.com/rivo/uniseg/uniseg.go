package uniseg

import "unicode/utf8"

// StringWidth is a small local fallback for this POC bundle.
// It is sufficient for terminal proofing and common CJK/emoji width, while
// keeping the bundle offline-buildable. Swap this module for upstream uniseg
// by removing the root go.mod replace when network/module cache is available.
func StringWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			w++
		case r < 0x20 || (r >= 0x7f && r < 0xa0):
			// control characters: width 0
		case r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
			(r >= 0x2e80 && r <= 0xa4cf) || (r >= 0xac00 && r <= 0xd7a3) ||
			(r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe10 && r <= 0xfe19) ||
			(r >= 0xfe30 && r <= 0xfe6f) || (r >= 0xff00 && r <= 0xff60) ||
			(r >= 0xffe0 && r <= 0xffe6) || (r >= 0x1f300 && r <= 0x1faff)):
			w += 2
		default:
			w++
		}
	}
	return w
}

// FirstGraphemeCluster returns the next UTF-8 rune as a minimal grapheme
// cluster fallback.
func FirstGraphemeCluster(b []byte, state int) (cluster []byte, rest []byte, newState int, boundaries int) {
	if len(b) == 0 {
		return nil, nil, 0, 0
	}
	_, size := utf8.DecodeRune(b)
	if size <= 0 {
		size = 1
	}
	return b[:size], b[size:], 0, 0
}
