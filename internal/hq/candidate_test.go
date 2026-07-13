package hq

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"hq/internal/core"
)

const selectedRecallIdentity = `{"kind":"hq.world.v1","world_id":"world.hq-recall-test"}`
const selectedRecallCommand = `{"kind":"hq.command.v1","command_id":"command.herdr.read","command_version":"v1","name":"herdr.read","aliases":["agent.read"],"keywords":["reviewer"],"description":"read agent output","instruction":{"version":"instruction.v1","op":"run","target":"herdr","payload":{"action":"read"}},"fields":[{"name":"agent","type":"string","required":true,"examples":["reviewer"],"bind":"payload.agent"},{"name":"source","type":"enum","required":true,"enum":["recent-unwrapped","screen"],"bind":"payload.source"},{"name":"lines","type":"integer","required":true,"default":100,"materialized_values":[50,200],"bind":"payload.lines"}],"presets":[{"id":"reviewer-screen","label":"Reviewer screen","values":{"agent":"reviewer","source":"screen","lines":100}}]}`

func selectedRecallWorld(t *testing.T) *JsonlWorld {
	t.Helper()
	world, err := LoadSelectedWorldJSONL(strings.NewReader(selectedRecallIdentity + "\n" + selectedRecallCommand))
	if err != nil {
		t.Fatal(err)
	}
	return world
}

func TestSelectedWorldRecallCandidateTransportGolden(t *testing.T) {
	world := selectedRecallWorld(t)
	recall, err := PrepareWorldRecall(world)
	if err != nil {
		t.Fatal(err)
	}
	query := "@ reviewer screen"
	suggestions := CompleteWithWorldRecall(query, len(query), 17, world, recall)
	if len(suggestions) != 2 {
		t.Fatalf("suggestions=%#v", suggestions)
	}
	const wantIndexID = "sha256:287cea6457dfeba9d6896752ca6c6d56248faeaaf9cbbd40cd48855c436e957f"
	wantCandidateIDs := []string{"sha256:e4af66171ece9d26ca3d68c44ed9311bfaa702932aacff682d1809b27375022b", "sha256:30043a7d6282aa5d9bcaa9b8ae0108a1a398b77be08e2ec8bf137aceb222c032"}
	if recall.ID() != wantIndexID {
		t.Fatalf("index id=%q", recall.ID())
	}
	for index, suggestion := range suggestions {
		if suggestion.Candidate == nil {
			t.Fatalf("suggestion %d has no candidate", index)
		}
		candidate := suggestion.Candidate
		if candidate.Kind != CandidateKind || candidate.IndexID != wantIndexID || candidate.CandidateID != wantCandidateIDs[index] {
			t.Fatalf("candidate %d identity=%#v", index, candidate)
		}
		if candidate.DocumentVersion != 17 || candidate.Edit != (CandidateEdit{StartByte: 0, EndByte: len(query), NewText: suggestion.InsertText}) {
			t.Fatalf("candidate %d request data=%#v", index, candidate)
		}
		if suggestion.Edit != (TextEdit{Start: candidate.Edit.StartByte, End: candidate.Edit.EndByte, Text: candidate.Edit.NewText}) {
			t.Fatalf("suggestion/candidate edit mismatch: %#v %#v", suggestion.Edit, candidate.Edit)
		}
		if suggestion.Draft.Kind != "" || suggestion.Draft.Queue != "" || suggestion.Draft.Instruction != nil {
			t.Fatalf("candidate selection acquired acceptance meaning=%#v", suggestion.Draft)
		}
		if !strings.HasPrefix(suggestion.SortText, "rank-") || !strings.HasSuffix(suggestion.SortText, candidate.CandidateID) {
			t.Fatalf("sortText does not transport rank and identity: %q", suggestion.SortText)
		}
		if !sort.SliceIsSorted(candidate.SourceRefs, func(i, j int) bool {
			if candidate.SourceRefs[i].TermKind != candidate.SourceRefs[j].TermKind {
				return candidate.SourceRefs[i].TermKind < candidate.SourceRefs[j].TermKind
			}
			return candidate.SourceRefs[i].TermPath < candidate.SourceRefs[j].TermPath
		}) {
			t.Fatalf("source refs are not sorted: %#v", candidate.SourceRefs)
		}
		for _, match := range candidate.Matches {
			if match.SourceRef != (CandidateSourceRef{TermKind: match.TermKind, TermPath: match.TermPath}) {
				t.Fatalf("match source ref=%#v match=%#v", match.SourceRef, match)
			}
		}
		assertCandidateJSON(t, *candidate)
	}
	if suggestions[0].SortText >= suggestions[1].SortText {
		t.Fatalf("structured rank order=%q %q", suggestions[0].SortText, suggestions[1].SortText)
	}
	results := recall.Recall(core.WorldRecallQuery{Scope: core.WorldRecallObjectQuery, Text: query})
	results[0], results[1] = results[1], results[0]
	reversed := recallSuggestions(recall, results, 0, len(query), query, 17)
	if reversed[0].Candidate.CandidateID != suggestions[1].Candidate.CandidateID || reversed[0].SortText != suggestions[1].SortText || reversed[1].SortText != suggestions[0].SortText {
		t.Fatalf("request ordinal changed sortText: %#v", reversed)
	}
}

func TestCandidateDocumentVersionAndUTF8EditAreRequestLocal(t *testing.T) {
	world := selectedRecallWorld(t)
	recall, err := PrepareWorldRecall(world)
	if err != nil {
		t.Fatal(err)
	}
	query := "@ reviewer screen"
	prefix := "😀\n"
	first := CompleteWithWorldRecall(query, len(query), 31, world, recall)
	versionChanged := CompleteWithWorldRecall(query, len(query), 32, world, recall)
	relocated := CompleteWithWorldRecall(prefix+query, len(prefix+query), 31, world, recall)
	if len(first) != len(versionChanged) || len(first) != len(relocated) || len(first) == 0 {
		t.Fatalf("completion sizes=%d %d %d", len(first), len(versionChanged), len(relocated))
	}
	for index := range first {
		left, right := *first[index].Candidate, *versionChanged[index].Candidate
		if left.CandidateID != right.CandidateID || left.IndexID != right.IndexID || first[index].SortText != versionChanged[index].SortText {
			t.Fatalf("request changed stable identity: %#v %#v", left, right)
		}
		if left.DocumentVersion != 31 || right.DocumentVersion != 32 {
			t.Fatalf("document versions=%d %d", left.DocumentVersion, right.DocumentVersion)
		}
		left.DocumentVersion = 0
		right.DocumentVersion = 0
		if !reflect.DeepEqual(left, right) {
			t.Fatalf("document version changed non-request data:\n%#v\n%#v", left, right)
		}

		baseCandidate, relocatedCandidate := first[index].Candidate, relocated[index].Candidate
		if baseCandidate.CandidateID != relocatedCandidate.CandidateID || baseCandidate.IndexID != relocatedCandidate.IndexID || first[index].SortText != relocated[index].SortText {
			t.Fatalf("edit relocation changed stable identity: %#v %#v", baseCandidate, relocatedCandidate)
		}
		if baseCandidate.Edit == relocatedCandidate.Edit || baseCandidate.Edit.StartByte != 0 || relocatedCandidate.Edit.StartByte != len(prefix) {
			t.Fatalf("edits are not request-local UTF-8 byte ranges: %#v %#v", baseCandidate.Edit, relocatedCandidate.Edit)
		}
	}
}

func TestLegacyWorldWithDiagnosticDigestNeverEmitsCandidate(t *testing.T) {
	world, err := LoadSchemaJSONL(strings.NewReader(recallCommand))
	if err != nil {
		t.Fatal(err)
	}
	if err := world.RecomputeDigest(); err != nil {
		t.Fatal(err)
	}
	recall, err := PrepareWorldRecall(world)
	if err != nil || recall != nil {
		t.Fatalf("legacy preparation=%#v err=%v", recall, err)
	}
	suggestions := Complete("@her", len("@her"), world)
	if len(suggestions) != 1 || suggestions[0].Candidate != nil || suggestions[0].Draft.Kind == "" {
		t.Fatalf("legacy suggestions=%#v", suggestions)
	}
}

func TestSelectedWorldPrepareErrorFailsClosed(t *testing.T) {
	world := selectedRecallWorld(t)
	world.Commands[0].CommandID = ""
	recall, err := PrepareWorldRecall(world)
	if err == nil || recall != nil {
		t.Fatalf("invalid selected preparation=%#v err=%v", recall, err)
	}
	if got := CompleteWithWorldRecall("@ reviewer", len("@ reviewer"), 1, world, recall); len(got) != 0 {
		t.Fatalf("failed preparation leaked partial recall=%#v", got)
	}
}

func TestSelectedWorldRecallRequiresExplicitObjectQuery(t *testing.T) {
	world := selectedRecallWorld(t)
	recall, err := PrepareWorldRecall(world)
	if err != nil {
		t.Fatal(err)
	}
	plain := "reviewer"
	for _, suggestion := range CompleteWithWorldRecall(plain, len(plain), 4, world, recall) {
		if suggestion.Candidate != nil {
			t.Fatalf("plain top-level text emitted recall candidate=%#v", suggestion)
		}
	}
	if IsMutableWorldRecall(plain, len(plain), world) {
		t.Fatal("plain top-level text reported mutable recall scope")
	}
	query := "@ reviewer"
	suggestions := CompleteWithWorldRecall(query, len(query), 4, world, recall)
	if len(suggestions) == 0 || suggestions[0].Candidate == nil {
		t.Fatalf("explicit object query emitted no candidate=%#v", suggestions)
	}
	if !IsMutableWorldRecall(query, len(query), world) {
		t.Fatal("explicit object query did not report mutable recall scope")
	}
}

func assertCandidateJSON(t *testing.T, candidate Candidate) {
	t.Helper()
	encoded, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Candidate
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundTrip, candidate) {
		t.Fatalf("candidate JSON round trip changed data:\n%s\n%#v", encoded, roundTrip)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"candidate_id", "candidate_kind", "command", "detail", "document_version", "documentation", "edit", "index_id", "kind", "label", "matches", "rank", "scope", "source_refs", "world"}
	gotKeys := make([]string, 0, len(object))
	for key := range object {
		gotKeys = append(gotKeys, key)
	}
	sort.Strings(gotKeys)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("candidate JSON fields=%v", gotKeys)
	}
	assertJSONKeys(t, object["edit"], []string{"end_byte", "new_text", "start_byte"})
	assertJSONKeys(t, object["rank"], []string{"candidate_kind", "direct_count", "exact_count", "prefix_count", "required_preference", "scope_compatibility", "subsequence_score", "substring_count", "worst_class"})
	var sourceRefs []json.RawMessage
	if err := json.Unmarshal(object["source_refs"], &sourceRefs); err != nil {
		t.Fatal(err)
	}
	for _, sourceRef := range sourceRefs {
		assertJSONKeys(t, sourceRef, []string{"term_kind", "term_path"})
	}
	var matches []map[string]json.RawMessage
	if err := json.Unmarshal(object["matches"], &matches); err != nil {
		t.Fatal(err)
	}
	for _, match := range matches {
		keys := make([]string, 0, len(match))
		for key := range match {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		wantMatchKeys := []string{"class", "positions", "primitive_score", "source_ref", "term_kind", "term_path", "token"}
		if !reflect.DeepEqual(keys, wantMatchKeys) {
			t.Fatalf("candidate match JSON fields=%v", keys)
		}
		assertJSONKeys(t, match["source_ref"], []string{"term_kind", "term_path"})
	}
	for _, forbidden := range []string{"compileDraft", "compile_draft", "deployment", "deployment_id", "effect", "execution", "history", "instruction", "materialization", "provider", "queue"} {
		if bytes.Contains(encoded, []byte(`"`+forbidden+`"`)) {
			t.Fatalf("candidate contains authority field %q: %s", forbidden, encoded)
		}
	}
}

func assertJSONKeys(t *testing.T, encoded json.RawMessage, want []string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON fields=%v want=%v", got, want)
	}
}
