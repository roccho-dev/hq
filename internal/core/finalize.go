package core

// AcceptedDraft wraps an adapter-provided instruction without interpreting or
// inventing domain fields. Default operations belong to schema adapters or
// their input data, not to core compilation mechanics.
func AcceptedDraft(inst map[string]any) CompileDraft {
	if inst == nil {
		inst = map[string]any{}
	}
	return CompileDraft{Kind: "accepted.instruction", Queue: "instruction.jsonl", Instruction: inst, Reason: "line accepted by human"}
}
