package core

func AcceptedDraft(inst map[string]any) CompileDraft {
	if inst == nil {
		inst = map[string]any{}
	}
	if _, ok := inst["op"]; !ok {
		inst["op"] = "queue.preview"
	}
	return CompileDraft{Kind: "accepted.instruction", Queue: "instruction.jsonl", Instruction: inst, Reason: "line accepted by human"}
}
