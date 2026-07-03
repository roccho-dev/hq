package hq

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type protocolFixture struct {
	Name       string        `json:"name"`
	SchemaJSONL string       `json:"schemaJSONL"`
	Cases      []fixtureCase `json:"cases"`
}

type fixtureCase struct {
	Name                        string   `json:"name"`
	Mode                        string   `json:"mode"`
	Buffer                      string   `json:"buffer"`
	Cursor                      int      `json:"cursor"`
	ExpectGoContextKind          string   `json:"expectGoContextKind"`
	ExpectPartial               string   `json:"expectPartial"`
	ExpectLabels                []string `json:"expectLabels"`
	ExpectDraftQueue            string   `json:"expectDraftQueue"`
	ExpectDraftKey              string   `json:"expectDraftKey"`
	ExpectAcceptedQueue         string   `json:"expectAcceptedQueue"`
	ExpectAcceptedInstructionOp string   `json:"expectAcceptedInstructionOp"`
}

func TestProtocolFixtureContract(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "spec", "fixtures", "protocol-contract.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture protocolFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	world, err := LoadSchemaJSONL(strings.NewReader(fixture.SchemaJSONL))
	if err != nil {
		t.Fatalf("load schema JSONL: %v", err)
	}

	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			switch tc.Mode {
			case "complete":
				ctx := Analyze(tc.Buffer, tc.Cursor, world)
				if string(ctx.Kind) != tc.ExpectGoContextKind {
					t.Fatalf("context kind = %q, want %q", ctx.Kind, tc.ExpectGoContextKind)
				}
				if ctx.Partial != tc.ExpectPartial {
					t.Fatalf("partial = %q, want %q", ctx.Partial, tc.ExpectPartial)
				}
				items := Complete(tc.Buffer, tc.Cursor, world)
				if len(items) < len(tc.ExpectLabels) {
					t.Fatalf("suggestions = %d, want at least %d", len(items), len(tc.ExpectLabels))
				}
				for i, want := range tc.ExpectLabels {
					got := normalizedLabel(items[i].Label)
					if got != want {
						t.Fatalf("label[%d] = %q, want %q", i, got, want)
					}
				}
				if tc.ExpectDraftQueue != "" && items[0].Draft.Queue != tc.ExpectDraftQueue {
					t.Fatalf("draft queue = %q, want %q", items[0].Draft.Queue, tc.ExpectDraftQueue)
				}
				if tc.ExpectDraftKey != "" && items[0].Draft.Key != tc.ExpectDraftKey {
					t.Fatalf("draft key = %q, want %q", items[0].Draft.Key, tc.ExpectDraftKey)
				}
			case "compile":
				draft := CompileLine(tc.Buffer, world)
				if draft.Queue != tc.ExpectAcceptedQueue {
					t.Fatalf("accepted queue = %q, want %q", draft.Queue, tc.ExpectAcceptedQueue)
				}
				if got := stringValue(draft.Instruction["op"]); got != tc.ExpectAcceptedInstructionOp {
					t.Fatalf("instruction op = %q, want %q", got, tc.ExpectAcceptedInstructionOp)
				}
			default:
				t.Fatalf("unknown fixture mode %q", tc.Mode)
			}
		})
	}
}

func normalizedLabel(label string) string {
	var unquoted string
	if err := json.Unmarshal([]byte(label), &unquoted); err == nil {
		return unquoted
	}
	return label
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
