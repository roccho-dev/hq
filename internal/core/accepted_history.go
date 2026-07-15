package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	AcceptedInputKind               = "hq.accepted-input.v1"
	HistoryPolicyContractVersion    = "hq.history-policy.v1"
	HistoryReductionContractVersion = "hq.accepted-history-reduction.v1"
	AcceptedHistoryProjectionKind   = "hq.accepted-history-projection.v1"
	WorldHistoryRecallRankVersion   = "hq.world-history-recall-rank.v1"
	HistoryPolicyDeny               = "deny"
	HistoryPolicyRecall             = "recall"
	HistoryPolicySearch             = "search"
	MaxAcceptedHistoryRows          = 1000
	MaxHistoryObjectPresets         = 100
	MaxHistoryValuesPerField        = 50
	MaxRecentHistoryPresets         = 20
	MaxCombinedRecallResults        = 50
	MaxHistorySources               = 5
	MaxAcceptedInputMaterialization = 32 << 10
	MaxHistoryDocumentationBytes    = 8 << 10
	MaxHistorySearchRunes           = 512
)

// AcceptedInputField is one policy-permitted typed value from the submitted
// @command object. Fields are stored in command declaration order.
type AcceptedInputField struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// AcceptedInput is non-authoritative recall evidence beside the canonical
// instruction. Instruction remains the only worker execution meaning.
type AcceptedInput struct {
	Kind                string               `json:"kind"`
	World               WorldRef             `json:"world"`
	Command             CommandRef           `json:"command"`
	Fields              []AcceptedInputField `json:"fields"`
	SuppliedFieldCount  int                  `json:"supplied_field_count"`
	RecallComplete      bool                 `json:"recall_complete"`
	RenderContract      string               `json:"render_contract"`
	InstructionDigest   string               `json:"instruction_digest"`
	AcceptedInputDigest string               `json:"accepted_input_digest"`
}

// AcceptedHistoryRecord is the normalized read-only input to the pure history
// reducer. It contains no source path, row number, or raw accepted bytes.
type AcceptedHistoryRecord struct {
	AcceptedID string
	AcceptedAt time.Time
	Provenance CompileProvenance
	Input      AcceptedInput
}

// AcceptedHistoryFinding is deliberately value-free. Reports may contain
// stable finding codes and counts only.
type AcceptedHistoryFinding struct {
	Code  string
	Count int
}

// AcceptedHistoryReport is in-memory evidence about projection degradation.
type AcceptedHistoryReport struct {
	Fatal    bool
	Findings []AcceptedHistoryFinding
}

func EffectiveHistoryPolicy(field CommandField) string {
	if field.HistoryPolicy == "" {
		return HistoryPolicyDeny
	}
	return field.HistoryPolicy
}

func HistoryPolicyPersists(field CommandField) bool {
	policy := EffectiveHistoryPolicy(field)
	return !field.Sensitive && (policy == HistoryPolicyRecall || policy == HistoryPolicySearch)
}

func HistoryPolicySearches(field CommandField) bool {
	return !field.Sensitive && EffectiveHistoryPolicy(field) == HistoryPolicySearch
}

func ValidHistoryPolicy(policy string) bool {
	return policy == "" || policy == HistoryPolicyDeny || policy == HistoryPolicyRecall || policy == HistoryPolicySearch
}

func (input *AcceptedInput) ApplyMaterializationLimit() error {
	if input == nil {
		return nil
	}
	encoded, err := json.Marshal(input.Fields)
	if err != nil {
		return err
	}
	if len(encoded) > MaxAcceptedInputMaterialization {
		input.Fields = nil
		input.RecallComplete = false
	}
	return nil
}

func (input *AcceptedInput) BindInstructionDigest(digest string) error {
	if input == nil {
		return nil
	}
	if !ValidDigest(digest) {
		return errors.New("accepted input requires a valid instruction digest")
	}
	input.InstructionDigest = digest
	input.AcceptedInputDigest = ""
	computed, err := CanonicalDigest(*input)
	if err != nil {
		return err
	}
	input.AcceptedInputDigest = computed
	return input.Validate()
}

func (input AcceptedInput) Validate() error {
	if input.Kind != AcceptedInputKind {
		return fmt.Errorf("accepted input kind must be %q", AcceptedInputKind)
	}
	if !ValidWorldRef(input.World) {
		return errors.New("accepted input world reference is invalid")
	}
	if !validRecallCommandRef(input.Command) {
		return errors.New("accepted input command reference is invalid")
	}
	if input.RenderContract != CommandObjectMaterializerVersion {
		return fmt.Errorf("accepted input render contract must be %q", CommandObjectMaterializerVersion)
	}
	if !ValidDigest(input.InstructionDigest) || !ValidDigest(input.AcceptedInputDigest) {
		return errors.New("accepted input digests are invalid")
	}
	if input.SuppliedFieldCount < 0 || input.SuppliedFieldCount < len(input.Fields) {
		return errors.New("accepted input supplied_field_count is invalid")
	}
	if input.RecallComplete != (input.SuppliedFieldCount == len(input.Fields)) {
		return errors.New("accepted input recall_complete does not match retained fields")
	}
	seen := map[string]bool{}
	for _, field := range input.Fields {
		if field.Name == "" || strings.TrimSpace(field.Name) != field.Name || seen[field.Name] {
			return errors.New("accepted input fields must have unique canonical names")
		}
		seen[field.Name] = true
		if field.Type != "string" && field.Type != "path" && field.Type != "enum" && field.Type != "integer" && field.Type != "boolean" {
			return fmt.Errorf("accepted input field %q has unsupported type %q", field.Name, field.Type)
		}
		if !acceptedInputValueMatchesType(field.Type, field.Value) {
			return fmt.Errorf("accepted input field %q value does not match type %q", field.Name, field.Type)
		}
	}
	copy := input
	copy.AcceptedInputDigest = ""
	computed, err := CanonicalDigest(copy)
	if err != nil {
		return err
	}
	if computed != input.AcceptedInputDigest {
		return errors.New("accepted input digest does not match content")
	}
	return nil
}

func ValidateAcceptedInputValue(field CommandField, value any) bool {
	if !acceptedInputValueMatchesType(field.Type, value) {
		return false
	}
	if field.Type == "enum" {
		text, _ := value.(string)
		for _, candidate := range field.Enum {
			if candidate == text {
				return true
			}
		}
		return false
	}
	return true
}

func AcceptedInputValueText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case json.Number:
		return typed.String()
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
	}
	return ""
}

func HistorySearchableText(value string) bool {
	return utf8.RuneCountInString(value) <= MaxHistorySearchRunes
}

func SortHistoryFindings(counts map[string]int) []AcceptedHistoryFinding {
	codes := make([]string, 0, len(counts))
	for code, count := range counts {
		if count > 0 {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	out := make([]AcceptedHistoryFinding, 0, len(codes))
	for _, code := range codes {
		out = append(out, AcceptedHistoryFinding{Code: code, Count: counts[code]})
	}
	return out
}

func acceptedInputValueMatchesType(kind string, value any) bool {
	switch kind {
	case "string", "path", "enum":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		switch typed := value.(type) {
		case json.Number:
			_, err := typed.Int64()
			return err == nil
		case int, int64:
			return true
		case float64:
			return typed == float64(int64(typed))
		}
	}
	return false
}
