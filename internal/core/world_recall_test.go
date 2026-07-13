package core_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"hq/internal/adapter/current"
	"hq/internal/core"
)

const recallIdentity = `{"kind":"hq.world.v1","world_id":"world.test"}`
const recallCatalog = `{"kind":"hq.command.v1","command_id":"command.catalog.read","command_version":"v1","name":"catalog.read","aliases":["entry.read"],"keywords":["inspection","日本語"],"description":"read catalog entries","instruction":{"version":"instruction.v1","op":"run","target":"catalog","payload":{"action":"read","options":{"quiet":true,"depth":2}}},"fields":[{"name":"item","type":"string","required":true,"description":"catalog item","examples":["reviewer"],"bind":"payload.item"},{"name":"mode","type":"enum","required":true,"description":"display mode","enum":["recent","screen"],"bind":"payload.mode"},{"name":"limit","type":"integer","required":true,"description":"result limit","default":100,"materialized_values":[50,200],"bind":"payload.limit"},{"name":"note","type":"string","description":"note","examples":["memo"],"bind":"payload.note"},{"name":"n_o_t_e","type":"string","required":true,"description":"required fuzzy key","examples":["required"],"bind":"payload.required"}],"presets":[{"id":"review-screen","label":"Pinned display","values":{"item":"reviewer","mode":"screen","limit":100,"n_o_t_e":"required"}}]}`
const recallCatalogObjectKeysReordered = `{"presets":[{"values":{"n_o_t_e":"required","limit":100,"mode":"screen","item":"reviewer"},"label":"Pinned display","id":"review-screen"}],"fields":[{"bind":"payload.item","examples":["reviewer"],"description":"catalog item","required":true,"type":"string","name":"item"},{"bind":"payload.mode","enum":["recent","screen"],"description":"display mode","required":true,"type":"enum","name":"mode"},{"bind":"payload.limit","materialized_values":[50,200],"default":100,"description":"result limit","required":true,"type":"integer","name":"limit"},{"bind":"payload.note","examples":["memo"],"description":"note","type":"string","name":"note"},{"bind":"payload.required","examples":["required"],"description":"required fuzzy key","required":true,"type":"string","name":"n_o_t_e"}],"instruction":{"payload":{"options":{"depth":2,"quiet":true},"action":"read"},"target":"catalog","op":"run","version":"instruction.v1"},"description":"read catalog entries","keywords":["inspection","日本語"],"aliases":["entry.read"],"name":"catalog.read","command_version":"v1","command_id":"command.catalog.read","kind":"hq.command.v1"}`
const recallWrite = `{"kind":"hq.command.v1","command_id":"command.catalog.write","command_version":"v1","name":"catalog.write","aliases":["entry.write"],"keywords":["update"],"description":"write catalog entries","instruction":{"version":"instruction.v1","op":"run","target":"catalog","payload":{"action":"write"}},"fields":[{"name":"destination","type":"string","required":true,"examples":["archive"],"bind":"payload.destination"}]}`

func recallWorld(t *testing.T, separator string, rows ...string) *core.JsonlWorld {
	t.Helper()
	world, err := current.LoadSelectedWorldJSONL(strings.NewReader(strings.Join(rows, separator)))
	if err != nil {
		t.Fatal(err)
	}
	return world
}

func prepareRecall(t *testing.T, world *core.JsonlWorld) *core.WorldRecallIndex {
	t.Helper()
	index, err := core.PrepareWorldRecall(world, recallTestIdentifier)
	if err != nil {
		t.Fatal(err)
	}
	return index
}

func TestWorldRecallWorldRowObjectKeyAndCRLFInvariance(t *testing.T) {
	worlds := []*core.JsonlWorld{
		recallWorld(t, "\n", recallIdentity, recallCatalog, recallWrite),
		recallWorld(t, "\r\n", recallWrite, recallCatalogObjectKeysReordered, recallIdentity),
	}
	queries := []string{"@ reviewer screen", "@ screen reviewer", "@ screen reviewer reviewer"}
	var goldenIndexID string
	var golden []byte
	for _, world := range worlds {
		index := prepareRecall(t, world)
		if goldenIndexID == "" {
			goldenIndexID = index.ID()
		} else if index.ID() != goldenIndexID {
			t.Fatalf("index identity changed: %q != %q", index.ID(), goldenIndexID)
		}
		for _, query := range queries {
			results := index.Recall(core.WorldRecallQuery{Scope: core.WorldRecallObjectQuery, Text: query})
			if len(results) != 2 || results[0].Kind != core.WorldRecallSchemaTemplate || results[1].Kind != core.WorldRecallObjectPreset {
				t.Fatalf("query %q results=%#v", query, results)
			}
			if results[0].Materialization != "@catalog.read\nitem=\nmode=\nlimit=100\nn_o_t_e=" {
				t.Fatalf("template=%q", results[0].Materialization)
			}
			if results[1].Materialization != "@catalog.read\nitem=reviewer\nmode=screen\nlimit=100\nn_o_t_e=required" {
				t.Fatalf("preset=%q", results[1].Materialization)
			}
			encoded, err := json.Marshal(results)
			if err != nil {
				t.Fatal(err)
			}
			if golden == nil {
				golden = encoded
			} else if !bytes.Equal(golden, encoded) {
				t.Fatalf("non-deterministic output\ngolden=%s\nactual=%s", golden, encoded)
			}
		}
	}
	if got := prepareRecall(t, recallWorld(t, "\n", recallIdentity, recallCatalog)).Recall(core.WorldRecallQuery{Scope: core.WorldRecallObjectQuery, Text: "reviewer absent"}); len(got) != 0 {
		t.Fatalf("AND semantics admitted=%#v", got)
	}
}

func TestWorldRecallCandidateIdentityIsQueryIndependent(t *testing.T) {
	index := prepareRecall(t, recallWorld(t, "\n", recallIdentity, recallCatalog))
	queries := []string{"", "reviewer", "screen", "screen reviewer"}
	var candidateID string
	for _, query := range queries {
		result, ok := resultKind(index.Recall(core.WorldRecallQuery{Scope: core.WorldRecallObjectQuery, Text: query}), core.WorldRecallObjectPreset)
		if !ok {
			t.Fatalf("query %q did not return preset", query)
		}
		if candidateID == "" {
			candidateID = result.CandidateID
		} else if result.CandidateID != candidateID {
			t.Fatalf("query %q changed candidate identity: %q != %q", query, result.CandidateID, candidateID)
		}
		if result.World.WorldID != "world.test" || result.Command.CommandID != "command.catalog.read" || !core.ValidDigest(result.CandidateID) {
			t.Fatalf("incomplete exact identity=%#v", result)
		}
	}
}

func TestWorldRecallEmitsAllMatchClassesAndUnicode(t *testing.T) {
	index := prepareRecall(t, recallWorld(t, "\n", recallIdentity, recallCatalog))
	cases := []struct {
		query string
		class core.WorldRecallMatchClass
	}{
		{"catalog.read", core.WorldRecallExact},
		{"catalog", core.WorldRecallPrefix},
		{"alog.re", core.WorldRecallSubstring},
		{"ctlgrd", core.WorldRecallSubsequence},
		{"日本語", core.WorldRecallExact},
	}
	for _, test := range cases {
		results := index.Recall(core.WorldRecallQuery{Scope: core.WorldRecallObjectQuery, Text: test.query})
		if len(results) == 0 || len(results[0].Matches) != 1 || results[0].Matches[0].Class != test.class {
			t.Fatalf("query %q results=%#v", test.query, results)
		}
		if len(results[0].Matches[0].Positions) == 0 {
			t.Fatalf("query %q has no positions", test.query)
		}
		if test.class == core.WorldRecallSubsequence && results[0].Matches[0].PrimitiveScore == 0 {
			t.Fatalf("fuzzy score=%d positions=%v", results[0].Matches[0].PrimitiveScore, results[0].Matches[0].Positions)
		}
	}
}

func TestWorldRecallMissingKeysAndAllWorldValueSources(t *testing.T) {
	index := prepareRecall(t, recallWorld(t, "\n", recallIdentity, recallCatalog))
	keys := index.Recall(core.WorldRecallQuery{
		Scope: core.WorldRecallMissingKey, CommandName: "catalog.read", Text: "note",
		PresentFields: map[string]bool{"item": true},
	})
	if len(keys) < 2 || keys[0].FieldName != "note" || keys[0].Rank.WorstClass != 0 {
		t.Fatalf("semantic evidence must beat required preference: %#v", keys)
	}
	for _, result := range keys {
		if result.FieldName == "item" {
			t.Fatalf("present key returned: %#v", result)
		}
	}
	values := index.Recall(core.WorldRecallQuery{Scope: core.WorldRecallFieldValue, CommandName: "catalog.read", FieldName: "limit"})
	got := make([]string, 0, len(values))
	for _, value := range values {
		got = append(got, value.Label)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"100", "200", "50"}) {
		t.Fatalf("default/materialized values=%v", got)
	}
	enum := index.Recall(core.WorldRecallQuery{Scope: core.WorldRecallFieldValue, CommandName: "catalog.read", FieldName: "mode", Text: "scr"})
	if len(enum) != 1 || enum[0].Label != "screen" || enum[0].Matches[0].TermKind != "field.enum" {
		t.Fatalf("enum=%#v", enum)
	}
}

func TestWorldRecallCoversTypedWorldTermSources(t *testing.T) {
	index := prepareRecall(t, recallWorld(t, "\n", recallIdentity, recallCatalog))
	templateTerms := map[string]string{
		"catalog.read": "command.name", "entry.read": "command.alias", "inspection": "command.keyword",
		"entries": "command.description", "limit": "field.name", "result": "field.description",
		"screen": "field.enum", "100": "field.default", "reviewer": "field.example", "200": "field.materialized",
	}
	for query, wantKind := range templateTerms {
		results := index.Recall(core.WorldRecallQuery{Scope: core.WorldRecallObjectQuery, Text: query})
		result, ok := resultKind(results, core.WorldRecallSchemaTemplate)
		if !ok || len(result.Matches) != 1 || result.Matches[0].TermKind != wantKind {
			t.Fatalf("query %q want %s results=%#v", query, wantKind, results)
		}
	}
	for query, wantKind := range map[string]string{"Pinned": "preset.label", "required": "preset.value"} {
		results := index.Recall(core.WorldRecallQuery{Scope: core.WorldRecallObjectQuery, Text: query})
		result, ok := resultKind(results, core.WorldRecallObjectPreset)
		if !ok || len(result.Matches) != 1 || result.Matches[0].TermKind != wantKind {
			t.Fatalf("query %q want %s results=%#v", query, wantKind, results)
		}
	}
}

func resultKind(results []core.WorldRecallResult, kind core.WorldRecallKind) (core.WorldRecallResult, bool) {
	for _, result := range results {
		if result.Kind == kind {
			return result, true
		}
	}
	return core.WorldRecallResult{}, false
}

func BenchmarkWorldRecall10000SearchableTerms(b *testing.B) {
	world := recallPerformanceWorld(b)
	prepared, err := core.PrepareWorldRecall(world, recallTestIdentifier)
	if err != nil {
		b.Fatal(err)
	}
	query := core.WorldRecallQuery{Scope: core.WorldRecallObjectQuery, Text: "needle"}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		results := prepared.Recall(query)
		if len(results) != 1 {
			b.Fatalf("results=%d", len(results))
		}
	}
}

func TestWorldRecall10000SearchableTermsReferenceP95(t *testing.T) {
	world := recallPerformanceWorld(t)
	fixture, err := json.Marshal(world.Commands)
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareRecall(t, world)
	query := core.WorldRecallQuery{Scope: core.WorldRecallObjectQuery, Text: "needle"}
	samples := make([]time.Duration, 100)
	for index := range samples {
		started := time.Now()
		results := prepared.Recall(query)
		samples[index] = time.Since(started)
		if len(results) != 1 {
			t.Fatalf("results=%d", len(results))
		}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[94]
	t.Logf("os=%s arch=%s go=%s fixture_sha256=%x p95=%s", runtime.GOOS, runtime.GOARCH, runtime.Version(), sha256.Sum256(fixture), p95)
	if p95 > 50*time.Millisecond {
		t.Fatalf("pure recall p95 %s exceeds 50ms reference gate", p95)
	}
}

type testingTB interface {
	Helper()
	Fatal(...any)
}

func recallPerformanceWorld(t testingTB) *core.JsonlWorld {
	t.Helper()
	world := &core.JsonlWorld{Identity: &core.WorldDefinition{Kind: core.WorldDefinitionKind, WorldID: "world.performance"}}
	for index := 0; index < 2500; index++ {
		keyword := fmt.Sprintf("keyword-%04d", index)
		if index == 2499 {
			keyword = "needle"
		}
		world.Commands = append(world.Commands, core.CommandDefinition{
			Kind: core.CommandInputKind, CommandID: fmt.Sprintf("command.%04d", index), CommandVersion: "v1",
			Name: fmt.Sprintf("command.%04d", index), Aliases: []string{fmt.Sprintf("alias-%04d", index)},
			Keywords: []string{keyword}, Description: fmt.Sprintf("description-%04d", index),
			Instruction: map[string]any{"op": "proof"},
		})
	}
	if err := world.RecomputeDigest(); err != nil {
		t.Fatal(err)
	}
	return world
}

func recallTestIdentifier(identity core.WorldRecallCandidateIdentity) (string, error) {
	return core.CanonicalDigest(identity)
}
