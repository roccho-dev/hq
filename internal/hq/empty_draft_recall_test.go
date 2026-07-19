package hq

import "testing"

func TestCanonicalEmptyRecallDraft(t *testing.T) {
	tests := []struct {
		name   string
		buffer string
		cursor int
		want   bool
	}{
		{name: "zero-byte document", buffer: "", cursor: 0, want: true},
		{name: "one empty LF line", buffer: "\n", cursor: 0, want: true},
		{name: "one empty CRLF line", buffer: "\r\n", cursor: 0, want: true},
		{name: "nonzero cursor", buffer: "", cursor: 1, want: false},
		{name: "space", buffer: " ", cursor: 0, want: false},
		{name: "tab", buffer: "\t", cursor: 0, want: false},
		{name: "whitespace line", buffer: " \n", cursor: 0, want: false},
		{name: "two LF lines", buffer: "\n\n", cursor: 0, want: false},
		{name: "two CRLF lines", buffer: "\r\n\r\n", cursor: 0, want: false},
		{name: "query", buffer: "@", cursor: 0, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canonicalEmptyRecallDraft(test.buffer, test.cursor); got != test.want {
				t.Fatalf("canonicalEmptyRecallDraft(%q, %d) = %v, want %v", test.buffer, test.cursor, got, test.want)
			}
		})
	}
}
