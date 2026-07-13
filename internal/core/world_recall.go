package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/sahilm/fuzzy"
)

const (
	WorldRecallCandidateIDKind       = "hq.candidate-id.v1"
	WorldRecallIndexKind             = "hq.world-recall-index.v1"
	WorldRecallNormalizationVersion  = "hq.recall-normalization.v1"
	WorldRecallMatcherModule         = "github.com/sahilm/fuzzy"
	WorldRecallMatcherVersion        = "v0.1.3"
	WorldRecallRankVersion           = "hq.world-recall-rank.v1"
	CommandObjectMaterializerVersion = "hq.command-object-materializer.v1"
)

type WorldRecallScope string

const (
	WorldRecallObjectQuery WorldRecallScope = "object_query"
	WorldRecallMissingKey  WorldRecallScope = "field_key"
	WorldRecallFieldValue  WorldRecallScope = "field_value"
)

type WorldRecallKind string

const (
	WorldRecallSchemaTemplate WorldRecallKind = "schema_template"
	WorldRecallObjectPreset   WorldRecallKind = "object_preset"
	WorldRecallMissingKeyKind WorldRecallKind = "missing_key"
	WorldRecallFieldValueKind WorldRecallKind = "field_value"
)

type WorldRecallMatchClass string

const (
	WorldRecallExact       WorldRecallMatchClass = "exact"
	WorldRecallPrefix      WorldRecallMatchClass = "prefix"
	WorldRecallSubstring   WorldRecallMatchClass = "substring"
	WorldRecallSubsequence WorldRecallMatchClass = "subsequence"
)

type WorldRecallStructuralRef struct {
	TermKind string
	TermPath string
}

type WorldRecallMatch struct {
	Token          string
	TermKind       string
	TermPath       string
	Class          WorldRecallMatchClass
	Positions      []int
	PrimitiveScore int
}

type WorldRecallRank struct {
	ScopeCompatibility int
	WorstClass         int
	ExactCount         int
	PrefixCount        int
	SubstringCount     int
	DirectCount        int
	SubsequenceScore   int
	RequiredPreference int
	CandidateKind      int
}

type WorldRecallQuery struct {
	Scope         WorldRecallScope
	Text          string
	CommandName   string
	FieldName     string
	PresentFields map[string]bool
}

type WorldRecallResult struct {
	CandidateID     string
	World           WorldRef
	Command         CommandRef
	StructuralRefs  []WorldRecallStructuralRef
	Scope           WorldRecallScope
	Kind            WorldRecallKind
	CommandName     string
	FieldName       string
	Label           string
	Detail          string
	Documentation   string
	Materialization string
	Matches         []WorldRecallMatch
	Rank            WorldRecallRank
}

// WorldRecallCandidateIdentity is the complete typed material needed by the
// owner of the candidate identity serialization contract. StructuralRefs are
// sorted and unique before this value is passed to the identifier.
type WorldRecallCandidateIdentity struct {
	Kind            string
	CandidateKind   WorldRecallKind
	World           WorldRef
	Command         CommandRef
	StructuralRefs  []WorldRecallStructuralRef
	Materialization string
}

type WorldRecallCandidateIdentifier func(WorldRecallCandidateIdentity) (string, error)

type recallTerm struct {
	Kind   string
	Path   string
	Text   string
	Folded string
}

type recallCandidate struct {
	CandidateID     string
	World           WorldRef
	Command         CommandRef
	StructuralRefs  []WorldRecallStructuralRef
	Scope           WorldRecallScope
	Kind            WorldRecallKind
	CommandName     string
	FieldName       string
	Label           string
	Detail          string
	Description     string
	Materialization string
	Required        bool
	Terms           []recallTerm
}

// WorldRecallIndex is immutable after successful preparation and contains
// only a projection of the selected semantic world.
type WorldRecallIndex struct {
	id         string
	world      WorldRef
	candidates []recallCandidate
}

func (index *WorldRecallIndex) ID() string {
	if index == nil {
		return ""
	}
	return index.id
}

// PrepareWorldRecall requires the strict identity contract of a selected
// world. It returns no partial index when an exact reference or generated
// candidate identity is invalid or duplicated.
func PrepareWorldRecall(world *JsonlWorld, identify WorldRecallCandidateIdentifier) (*WorldRecallIndex, error) {
	if identify == nil {
		return nil, errors.New("prepare world recall: candidate identifier is required")
	}
	worldRef, err := exactRecallWorldRef(world)
	if err != nil {
		return nil, err
	}
	indexID, err := worldRecallIndexID(worldRef.Digest)
	if err != nil {
		return nil, fmt.Errorf("world recall index identity: %w", err)
	}
	index := &WorldRecallIndex{id: indexID, world: worldRef}
	for _, command := range world.Commands {
		commandDigest, digestErr := CanonicalDigest(command)
		commandRef := CommandRef{
			CommandID: command.CommandID, CommandVersion: command.CommandVersion,
			Name: command.Name, Digest: commandDigest,
		}
		if digestErr != nil || command.Kind != CommandInputKind || !validRecallCommandRef(commandRef) {
			return nil, fmt.Errorf("world recall command %q has invalid exact identity", command.Name)
		}
		commandTerms := recallCommandTerms(command)
		templateTerms := append([]recallTerm{}, commandTerms...)
		for _, field := range command.Fields {
			templateTerms = append(templateTerms, recallFieldTerms(command.Name, field)...)
		}
		template := renderSchemaTemplate(command)
		index.candidates = append(index.candidates, recallCandidate{
			World: worldRef, Command: commandRef,
			Scope: WorldRecallObjectQuery, Kind: WorldRecallSchemaTemplate,
			CommandName: command.Name, Label: command.Name,
			Detail:      "schema_template | world declaration",
			Description: command.Description, Materialization: template,
			Terms: templateTerms,
		})
		for _, preset := range command.Presets {
			materialization := renderPreset(command, preset)
			terms := append([]recallTerm{}, commandTerms...)
			terms = append(terms, recallPresetTerms(command, preset)...)
			index.candidates = append(index.candidates, recallCandidate{
				World: worldRef, Command: commandRef,
				Scope: WorldRecallObjectQuery, Kind: WorldRecallObjectPreset,
				CommandName: command.Name, Label: preset.Label,
				Detail:      "object_preset | explicit world preset",
				Description: command.Description, Materialization: materialization,
				Terms: terms,
			})
		}
		for _, field := range command.Fields {
			terms := recallFieldTerms(command.Name, field)
			index.candidates = append(index.candidates, recallCandidate{
				World: worldRef, Command: commandRef,
				Scope: WorldRecallMissingKey, Kind: WorldRecallMissingKeyKind,
				CommandName: command.Name, FieldName: field.Name, Label: field.Name,
				Detail:      "missing_key | " + field.Type + " | world declaration",
				Description: field.Description, Materialization: field.Name + "=", Required: field.Required,
				Terms: terms,
			})
			for _, value := range recallFieldValues(field) {
				terms := make([]recallTerm, 0, len(value.Sources))
				for _, source := range value.Sources {
					terms = append(terms, recallTerm{Kind: source.Kind, Path: recallFieldPath(command.Name, field.Name) + "." + source.Path, Text: source.Text})
				}
				index.candidates = append(index.candidates, recallCandidate{
					World: worldRef, Command: commandRef,
					Scope: WorldRecallFieldValue, Kind: WorldRecallFieldValueKind,
					CommandName: command.Name, FieldName: field.Name, Label: value.Text,
					Detail:      "field_value | sources: " + recallFieldValueSourceDetail(value.Sources) + " | world declaration",
					Description: field.Description, Materialization: quoteCommandMaterial(value.Text),
					Terms: terms,
				})
			}
		}
	}
	seen := make(map[string]struct{}, len(index.candidates))
	for candidateIndex := range index.candidates {
		candidate := &index.candidates[candidateIndex]
		prepareRecallTerms(candidate)
		candidate.StructuralRefs = recallStructuralRefs(candidate.Terms)
		candidate.CandidateID, err = identify(WorldRecallCandidateIdentity{
			Kind: WorldRecallCandidateIDKind, CandidateKind: candidate.Kind,
			World: candidate.World, Command: candidate.Command,
			StructuralRefs:  append([]WorldRecallStructuralRef(nil), candidate.StructuralRefs...),
			Materialization: candidate.Materialization,
		})
		if err != nil || !ValidDigest(candidate.CandidateID) {
			if err == nil {
				err = errors.New("invalid canonical digest")
			}
			return nil, fmt.Errorf("world recall candidate identity: %w", err)
		}
		if _, duplicate := seen[candidate.CandidateID]; duplicate {
			return nil, fmt.Errorf("duplicate world recall candidate identity %q", candidate.CandidateID)
		}
		seen[candidate.CandidateID] = struct{}{}
	}
	sort.Slice(index.candidates, func(i, j int) bool {
		return index.candidates[i].CandidateID < index.candidates[j].CandidateID
	})
	return index, nil
}

func (index *WorldRecallIndex) Recall(query WorldRecallQuery) []WorldRecallResult {
	if index == nil {
		return nil
	}
	tokens := normalizeRecallQuery(query.Text)
	results := make([]WorldRecallResult, 0)
	for _, candidate := range index.candidates {
		if candidate.Scope != query.Scope || (query.CommandName != "" && candidate.CommandName != query.CommandName) || (query.FieldName != "" && candidate.FieldName != query.FieldName) {
			continue
		}
		if candidate.Kind == WorldRecallMissingKeyKind && query.PresentFields[candidate.FieldName] {
			continue
		}
		matches, ok := recallMatches(tokens, candidate.Terms)
		if !ok {
			continue
		}
		rank := recallRank(candidate, matches)
		results = append(results, WorldRecallResult{
			CandidateID: candidate.CandidateID, World: candidate.World, Command: candidate.Command,
			StructuralRefs: append([]WorldRecallStructuralRef(nil), candidate.StructuralRefs...),
			Scope:          candidate.Scope, Kind: candidate.Kind, CommandName: candidate.CommandName,
			FieldName: candidate.FieldName, Label: candidate.Label, Detail: candidate.Detail,
			Documentation: recallDocumentation(candidate, matches), Materialization: candidate.Materialization,
			Matches: matches, Rank: rank,
		})
	}
	sort.Slice(results, func(i, j int) bool { return lessRecallResult(results[i], results[j]) })
	return results
}

func exactRecallWorldRef(world *JsonlWorld) (WorldRef, error) {
	if err := world.ValidateSelected(); err != nil {
		return WorldRef{}, fmt.Errorf("prepare world recall: %w", err)
	}
	ref, ok := world.SelectedRef()
	if !ok || !validStableIdentity(ref.WorldID) || !ValidDigest(ref.Digest) {
		return WorldRef{}, errors.New("prepare world recall: invalid exact world reference")
	}
	semantic := *world
	semantic.Digest = ""
	if err := semantic.RecomputeDigest(); err != nil {
		return WorldRef{}, fmt.Errorf("prepare world recall: recompute world digest: %w", err)
	}
	if semantic.Digest != ref.Digest {
		return WorldRef{}, errors.New("prepare world recall: selected world digest does not match semantic content")
	}
	return ref, nil
}

func validRecallCommandRef(ref CommandRef) bool {
	return validStableIdentity(ref.CommandID) && validStableIdentity(ref.CommandVersion) && strings.TrimSpace(ref.Name) != "" && ValidDigest(ref.Digest)
}

type recallMatcherIdentity struct {
	Module  string `json:"module"`
	Version string `json:"version"`
}

type recallIndexIDPreimage struct {
	Kind                 string                `json:"kind"`
	WorldDigest          string                `json:"world_digest"`
	NormalizationVersion string                `json:"normalization_version"`
	Matcher              recallMatcherIdentity `json:"matcher"`
	RankVersion          string                `json:"rank_version"`
	MaterializerVersion  string                `json:"materializer_version"`
}

func worldRecallIndexID(worldDigest string) (string, error) {
	return CanonicalDigest(recallIndexIDPreimage{
		Kind: WorldRecallIndexKind, WorldDigest: worldDigest,
		NormalizationVersion: WorldRecallNormalizationVersion,
		Matcher:              recallMatcherIdentity{Module: WorldRecallMatcherModule, Version: WorldRecallMatcherVersion},
		RankVersion:          WorldRecallRankVersion, MaterializerVersion: CommandObjectMaterializerVersion,
	})
}

func prepareRecallTerms(candidate *recallCandidate) {
	for index := range candidate.Terms {
		candidate.Terms[index].Folded = strings.ToLower(candidate.Terms[index].Text)
	}
	sort.Slice(candidate.Terms, func(i, j int) bool {
		left, right := candidate.Terms[i], candidate.Terms[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Text < right.Text
	})
	unique := candidate.Terms[:0]
	for _, term := range candidate.Terms {
		if len(unique) == 0 || unique[len(unique)-1].Kind != term.Kind || unique[len(unique)-1].Path != term.Path || unique[len(unique)-1].Text != term.Text {
			unique = append(unique, term)
		}
	}
	candidate.Terms = unique
}

func recallStructuralRefs(terms []recallTerm) []WorldRecallStructuralRef {
	refs := make([]WorldRecallStructuralRef, 0, len(terms))
	for _, term := range terms {
		ref := WorldRecallStructuralRef{TermKind: term.Kind, TermPath: term.Path}
		if len(refs) == 0 || refs[len(refs)-1] != ref {
			refs = append(refs, ref)
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].TermKind != refs[j].TermKind {
			return refs[i].TermKind < refs[j].TermKind
		}
		return refs[i].TermPath < refs[j].TermPath
	})
	unique := refs[:0]
	for _, ref := range refs {
		if len(unique) == 0 || unique[len(unique)-1] != ref {
			unique = append(unique, ref)
		}
	}
	return unique
}

func normalizeRecallQuery(input string) []string {
	input = strings.TrimPrefix(input, "@")
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(input)))
	seen := map[string]bool{}
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		if field != "" && !seen[field] {
			seen[field] = true
			tokens = append(tokens, field)
		}
	}
	sort.Strings(tokens)
	return tokens
}

func recallMatches(tokens []string, terms []recallTerm) ([]WorldRecallMatch, bool) {
	matches := make([]WorldRecallMatch, 0, len(tokens))
	for _, token := range tokens {
		var best WorldRecallMatch
		found := false
		for _, term := range terms {
			match, ok := recallTermMatch(token, term)
			if !ok {
				continue
			}
			if !found || lessRecallMatch(match, best) {
				best = match
				found = true
			}
		}
		if !found {
			return nil, false
		}
		matches = append(matches, best)
	}
	return matches, true
}

func recallTermMatch(token string, term recallTerm) (WorldRecallMatch, bool) {
	text := term.Folded
	match := WorldRecallMatch{Token: token, TermKind: term.Kind, TermPath: term.Path}
	switch {
	case text == token:
		match.Class = WorldRecallExact
		match.Positions = runeRange(0, utf8.RuneCountInString(token))
		return match, true
	case strings.HasPrefix(text, token):
		match.Class = WorldRecallPrefix
		match.Positions = runeRange(0, utf8.RuneCountInString(token))
		return match, true
	case strings.Contains(text, token):
		match.Class = WorldRecallSubstring
		byteStart := strings.Index(text, token)
		runeStart := utf8.RuneCountInString(text[:byteStart])
		match.Positions = runeRange(runeStart, utf8.RuneCountInString(token))
		return match, true
	default:
		found := fuzzy.FindNoSort(token, []string{text})
		if len(found) != 1 {
			return WorldRecallMatch{}, false
		}
		match.Class = WorldRecallSubsequence
		match.PrimitiveScore = found[0].Score
		match.Positions = bytePositionsToRunes(text, found[0].MatchedIndexes)
		return match, true
	}
}

func lessRecallMatch(left, right WorldRecallMatch) bool {
	if recallClassOrder(left.Class) != recallClassOrder(right.Class) {
		return recallClassOrder(left.Class) < recallClassOrder(right.Class)
	}
	if left.Class == WorldRecallSubsequence && left.PrimitiveScore != right.PrimitiveScore {
		return left.PrimitiveScore > right.PrimitiveScore
	}
	if recallTermKindOrder(left.TermKind) != recallTermKindOrder(right.TermKind) {
		return recallTermKindOrder(left.TermKind) < recallTermKindOrder(right.TermKind)
	}
	return left.TermPath < right.TermPath
}

func recallRank(candidate recallCandidate, matches []WorldRecallMatch) WorldRecallRank {
	rank := WorldRecallRank{ScopeCompatibility: 0, CandidateKind: recallCandidateKindOrder(candidate.Kind)}
	if len(matches) > 0 {
		rank.WorstClass = -1
	}
	for _, match := range matches {
		class := recallClassOrder(match.Class)
		if class > rank.WorstClass {
			rank.WorstClass = class
		}
		switch match.Class {
		case WorldRecallExact:
			rank.ExactCount++
		case WorldRecallPrefix:
			rank.PrefixCount++
		case WorldRecallSubstring:
			rank.SubstringCount++
		case WorldRecallSubsequence:
			rank.SubsequenceScore += match.PrimitiveScore
		}
		if !recallDescriptionKind(match.TermKind) {
			rank.DirectCount++
		}
	}
	if candidate.Kind == WorldRecallMissingKeyKind && candidate.Required {
		rank.RequiredPreference = 1
	}
	return rank
}

func lessRecallResult(left, right WorldRecallResult) bool {
	a, b := left.Rank, right.Rank
	if a.ScopeCompatibility != b.ScopeCompatibility {
		return a.ScopeCompatibility < b.ScopeCompatibility
	}
	if a.WorstClass != b.WorstClass {
		return a.WorstClass < b.WorstClass
	}
	if a.ExactCount != b.ExactCount {
		return a.ExactCount > b.ExactCount
	}
	if a.PrefixCount != b.PrefixCount {
		return a.PrefixCount > b.PrefixCount
	}
	if a.SubstringCount != b.SubstringCount {
		return a.SubstringCount > b.SubstringCount
	}
	if a.DirectCount != b.DirectCount {
		return a.DirectCount > b.DirectCount
	}
	if a.SubsequenceScore != b.SubsequenceScore {
		return a.SubsequenceScore > b.SubsequenceScore
	}
	if a.RequiredPreference != b.RequiredPreference {
		return a.RequiredPreference > b.RequiredPreference
	}
	if a.CandidateKind != b.CandidateKind {
		return a.CandidateKind < b.CandidateKind
	}
	return left.CandidateID < right.CandidateID
}

func recallDocumentation(candidate recallCandidate, matches []WorldRecallMatch) string {
	parts := []string{}
	if candidate.Description != "" {
		parts = append(parts, candidate.Description)
	}
	parts = append(parts, "Preview:\n"+candidate.Materialization)
	if len(matches) > 0 {
		reasons := make([]string, 0, len(matches))
		for _, match := range matches {
			reasons = append(reasons, fmt.Sprintf("%s: %s via %s (%s)", match.Token, match.Class, match.TermKind, match.TermPath))
		}
		parts = append(parts, "Matches:\n"+strings.Join(reasons, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

func recallCommandTerms(command CommandDefinition) []recallTerm {
	path := recallTypedPath("command", command.Name)
	terms := []recallTerm{{Kind: "command.name", Path: path + ".name", Text: command.Name}}
	for _, value := range command.Aliases {
		terms = append(terms, recallTerm{Kind: "command.alias", Path: path + "." + recallTypedPath("alias", value), Text: value})
	}
	for _, value := range command.Keywords {
		terms = append(terms, recallTerm{Kind: "command.keyword", Path: path + "." + recallTypedPath("keyword", value), Text: value})
	}
	if command.Description != "" {
		terms = append(terms, recallTerm{Kind: "command.description", Path: path + ".description", Text: command.Description})
	}
	return terms
}

func recallFieldTerms(commandName string, field CommandField) []recallTerm {
	path := recallFieldPath(commandName, field.Name)
	terms := []recallTerm{{Kind: "field.name", Path: path + ".name", Text: field.Name}}
	if field.Description != "" {
		terms = append(terms, recallTerm{Kind: "field.description", Path: path + ".description", Text: field.Description})
	}
	for _, value := range recallFieldValueTerms(field) {
		terms = append(terms, recallTerm{Kind: value.Kind, Path: path + "." + value.Path, Text: value.Text})
	}
	return terms
}

func recallPresetTerms(command CommandDefinition, preset CommandPreset) []recallTerm {
	base := recallTypedPath("command", command.Name) + "." + recallTypedPath("preset", preset.ID)
	terms := []recallTerm{{Kind: "preset.label", Path: base + ".label", Text: preset.Label}}
	for _, field := range command.Fields {
		value, ok := preset.Values[field.Name]
		if !ok && field.Default != nil {
			value, ok = *field.Default, true
		}
		if ok {
			terms = append(terms, recallTerm{Kind: "preset.value", Path: base + "." + recallTypedPath("value", field.Name), Text: value.Text()})
		}
	}
	return terms
}

type recallFieldValueTerm struct{ Kind, Path, Text string }

type recallFieldValue struct {
	Text    string
	Sources []recallFieldValueTerm
}

func recallFieldValueTerms(field CommandField) []recallFieldValueTerm {
	values := []recallFieldValueTerm{}
	for _, value := range field.Enum {
		values = append(values, recallFieldValueTerm{"field.enum", recallTypedPath("enum", value), value})
	}
	if field.Default != nil {
		values = append(values, recallFieldValueTerm{"field.default", "default", field.Default.Text()})
	}
	for _, value := range field.Examples {
		values = append(values, recallFieldValueTerm{"field.example", recallTypedPath("example", value), value})
	}
	for _, value := range field.MaterializedValues {
		values = append(values, recallFieldValueTerm{"field.materialized", recallTypedPath("materialized", value.Text()), value.Text()})
	}
	return values
}

func recallFieldValues(field CommandField) []recallFieldValue {
	groups := []recallFieldValue{}
	byText := map[string]int{}
	for _, source := range recallFieldValueTerms(field) {
		position, exists := byText[source.Text]
		if !exists {
			position = len(groups)
			byText[source.Text] = position
			groups = append(groups, recallFieldValue{Text: source.Text})
		}
		groups[position].Sources = append(groups[position].Sources, source)
	}
	return groups
}

func recallFieldValueSourceDetail(sources []recallFieldValueTerm) string {
	kinds := make([]string, 0, len(sources))
	seen := map[string]bool{}
	for _, source := range sources {
		if !seen[source.Kind] {
			seen[source.Kind] = true
			kinds = append(kinds, source.Kind)
		}
	}
	return strings.Join(kinds, " > ")
}

func renderSchemaTemplate(command CommandDefinition) string {
	lines := []string{"@" + command.Name}
	for _, field := range command.Fields {
		if !field.Required {
			continue
		}
		value := ""
		if field.Default != nil {
			value = quoteCommandMaterial(field.Default.Text())
		}
		lines = append(lines, field.Name+"="+value)
	}
	return strings.Join(lines, "\n")
}

func renderPreset(command CommandDefinition, preset CommandPreset) string {
	lines := []string{"@" + command.Name}
	for _, field := range command.Fields {
		value, ok := preset.Values[field.Name]
		if !ok && field.Required && field.Default != nil {
			value, ok = *field.Default, true
		}
		if ok {
			lines = append(lines, field.Name+"="+quoteCommandMaterial(value.Text()))
		}
	}
	return strings.Join(lines, "\n")
}

func quoteCommandMaterial(value string) string {
	if strings.ContainsAny(value, " \t") {
		return `"` + value + `"`
	}
	return value
}

func recallFieldPath(command, field string) string {
	return recallTypedPath("command", command) + "." + recallTypedPath("field", field)
}

func recallTypedPath(kind, component string) string {
	return kind + "[" + canonicalRecallComponent(component) + "]"
}

func canonicalRecallComponent(component string) string {
	encoded, _ := json.Marshal(component)
	return string(encoded)
}

func runeRange(start, length int) []int {
	out := make([]int, length)
	for i := range out {
		out[i] = start + i
	}
	return out
}

func bytePositionsToRunes(text string, positions []int) []int {
	out := make([]int, len(positions))
	for i, position := range positions {
		out[i] = utf8.RuneCountInString(text[:position])
	}
	return out
}

func recallClassOrder(class WorldRecallMatchClass) int {
	switch class {
	case WorldRecallExact:
		return 0
	case WorldRecallPrefix:
		return 1
	case WorldRecallSubstring:
		return 2
	default:
		return 3
	}
}

func recallCandidateKindOrder(kind WorldRecallKind) int {
	switch kind {
	case WorldRecallSchemaTemplate:
		return 0
	case WorldRecallObjectPreset:
		return 1
	case WorldRecallMissingKeyKind:
		return 2
	default:
		return 3
	}
}

func recallDescriptionKind(kind string) bool {
	return kind == "command.description" || kind == "field.description"
}

func recallTermKindOrder(kind string) int {
	order := map[string]int{"command.name": 0, "command.alias": 1, "command.keyword": 2, "field.name": 3, "field.enum": 4, "field.default": 5, "field.example": 6, "field.materialized": 7, "preset.label": 8, "preset.value": 9, "command.description": 10, "field.description": 11}
	if value, ok := order[kind]; ok {
		return value
	}
	return len(order)
}
