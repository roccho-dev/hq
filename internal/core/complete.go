package core

func Complete(buffer string, cursor int, world *JsonlWorld) []Suggestion {
	if world == nil || len(world.Keys) == 0 {
		return nil
	}
	ctx := Analyze(buffer, cursor, world)
	out := make([]Suggestion, 0, len(world.Keys))
	for _, k := range world.Keys {
		out = append(out, Suggestion{Label: k.Key, InsertText: k.Key, Detail: k.Type, Description: k.Description, Tag: k.Group, Score: 1, Draft: CompileDraft{Kind: "candidate.key", Queue: "instruction.jsonl", Key: k.Key, Instruction: map[string]any{"key": k.Key}, Reason: string(ctx.Kind)}})
	}
	return out
}
