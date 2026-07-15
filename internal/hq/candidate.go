package hq

import (
	"fmt"
	"sort"

	"hq/internal/core"
)

const CandidateKind = "hq.candidate.v1"

type CandidateSourceRef struct {
	TermKind string `json:"term_kind"`
	TermPath string `json:"term_path"`
}

type CandidateMatch struct {
	Token          string                     `json:"token"`
	TermKind       string                     `json:"term_kind"`
	TermPath       string                     `json:"term_path"`
	Class          core.WorldRecallMatchClass `json:"class"`
	Positions      []int                      `json:"positions"`
	PrimitiveScore int                        `json:"primitive_score"`
	SourceRef      CandidateSourceRef         `json:"source_ref"`
}

type CandidateRank struct {
	ScopeCompatibility int   `json:"scope_compatibility"`
	WorstClass         int   `json:"worst_class"`
	ExactCount         int   `json:"exact_count"`
	PrefixCount        int   `json:"prefix_count"`
	SubstringCount     int   `json:"substring_count"`
	DirectCount        int   `json:"direct_count"`
	SubsequenceScore   int   `json:"subsequence_score"`
	RequiredPreference int   `json:"required_preference"`
	SourcePreference   int   `json:"source_preference,omitempty"`
	CandidateKind      int   `json:"candidate_kind"`
	HistoryRecency     int64 `json:"history_recency,omitempty"`
	HistoryFrequency   int   `json:"history_frequency,omitempty"`
}

type CandidateEdit struct {
	StartByte int    `json:"start_byte"`
	EndByte   int    `json:"end_byte"`
	NewText   string `json:"new_text"`
}

type Candidate struct {
	Kind            string                `json:"kind"`
	CandidateID     string                `json:"candidate_id"`
	IndexID         string                `json:"index_id"`
	DocumentVersion int                   `json:"document_version"`
	Scope           core.WorldRecallScope `json:"scope"`
	CandidateKind   core.WorldRecallKind  `json:"candidate_kind"`
	Label           string                `json:"label"`
	Detail          string                `json:"detail"`
	Documentation   string                `json:"documentation"`
	World           WorldRef              `json:"world"`
	Command         CommandRef            `json:"command"`
	SourceRefs      []CandidateSourceRef  `json:"source_refs"`
	Matches         []CandidateMatch      `json:"matches"`
	Rank            CandidateRank         `json:"rank"`
	Edit            CandidateEdit         `json:"edit"`
}

type candidateIDPreimage struct {
	Kind            string               `json:"kind"`
	CandidateKind   core.WorldRecallKind `json:"candidate_kind"`
	World           WorldRef             `json:"world"`
	Command         CommandRef           `json:"command"`
	SourceRefs      []CandidateSourceRef `json:"source_refs"`
	Materialization string               `json:"materialization"`
}

func PrepareWorldRecall(world *JsonlWorld) (*core.WorldRecallIndex, error) {
	if world == nil || world.Identity == nil {
		return nil, nil
	}
	if _, selected := world.SelectedRef(); !selected {
		return nil, fmt.Errorf("prepare world recall: world is neither identity-free nor an exact selected world")
	}
	for _, command := range world.Commands {
		if _, exact := world.CommandRef(command.Name); !exact {
			return nil, fmt.Errorf("prepare world recall: command %q has no exact reference", command.Name)
		}
	}
	return core.PrepareWorldRecall(world, candidateID)
}

func candidateID(identity core.WorldRecallCandidateIdentity) (string, error) {
	refs := candidateSourceRefs(identity.StructuralRefs)
	return core.CanonicalDigest(candidateIDPreimage{
		Kind: identity.Kind, CandidateKind: identity.CandidateKind,
		World: identity.World, Command: identity.Command,
		SourceRefs: refs, Materialization: identity.Materialization,
	})
}

func candidateSourceRefs(refs []core.WorldRecallStructuralRef) []CandidateSourceRef {
	out := make([]CandidateSourceRef, len(refs))
	for index, ref := range refs {
		out[index] = CandidateSourceRef{TermKind: ref.TermKind, TermPath: ref.TermPath}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TermKind != out[j].TermKind {
			return out[i].TermKind < out[j].TermKind
		}
		return out[i].TermPath < out[j].TermPath
	})
	return out
}

func candidateRank(rank core.WorldRecallRank) CandidateRank {
	return CandidateRank{
		ScopeCompatibility: rank.ScopeCompatibility, WorstClass: rank.WorstClass,
		ExactCount: rank.ExactCount, PrefixCount: rank.PrefixCount,
		SubstringCount: rank.SubstringCount, DirectCount: rank.DirectCount,
		SubsequenceScore: rank.SubsequenceScore, RequiredPreference: rank.RequiredPreference,
		SourcePreference: rank.SourcePreference, CandidateKind: rank.CandidateKind,
		HistoryRecency: rank.HistoryRecency, HistoryFrequency: rank.HistoryFrequency,
	}
}
