package localtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"hq/internal/worker/adapter"
)

// RunViewGateway materializes a required view through another finite local-tool
// action from the same selected world. It does not own run state; the view reads
// the canonical result log identified by EventsPath.
type RunViewGateway struct {
	Preparer   Preparer
	EventsPath string
}

func (g RunViewGateway) Open(ctx context.Context, request adapter.Request) (*adapter.RunView, error) {
	if request.Target != "local-tool" || request.Operation != "run" {
		return nil, nil
	}
	var value payload
	if err := decodeStrict(request.Payload, &value); err != nil {
		return nil, err
	}
	if value.ActionID == "" || value.Input == nil || g.Preparer.World == nil {
		return nil, nil
	}
	tool, ok := g.Preparer.World.LocalTool(value.ToolID, value.ToolVersion)
	if !ok {
		return nil, nil
	}
	action, ok := actionByID(tool, value.ActionID)
	if !ok || action.RunView == nil {
		return nil, nil
	}
	view := action.RunView
	if view.Policy != "required" {
		return nil, errors.New("unsupported run view policy")
	}
	if strings.TrimSpace(g.EventsPath) == "" {
		return nil, errors.New("canonical event path is unavailable")
	}
	viewTool, ok := g.Preparer.World.LocalTool(view.ToolID, view.ToolVersion)
	if !ok {
		return nil, fmt.Errorf("run view tool %q version %q is unavailable", view.ToolID, view.ToolVersion)
	}
	viewAction, ok := actionByID(viewTool, view.ActionID)
	if !ok {
		return nil, fmt.Errorf("run view action %q is unavailable", view.ActionID)
	}
	if viewAction.RunView != nil {
		return nil, errors.New("run view action must not declare another run view")
	}
	viewPayload, err := json.Marshal(payload{
		ToolID:      view.ToolID,
		ToolVersion: view.ToolVersion,
		ActionID:    view.ActionID,
		Input: map[string]json.RawMessage{
			"view_id":     stringRaw("hq-" + request.RunID),
			"run_id":      stringRaw(request.RunID),
			"events_path": stringRaw(g.EventsPath),
		},
	})
	if err != nil {
		return nil, err
	}
	viewRequest := adapter.Request{
		RunID: request.RunID, InstructionID: request.InstructionID,
		Target: "local-tool", Operation: "run", Payload: viewPayload, CWD: request.CWD,
	}
	prepared, err := g.Preparer.Prepare(ctx, viewRequest)
	if err != nil {
		return nil, err
	}
	if prepared.RunViewRequired {
		return nil, errors.New("recursive required run view is not permitted")
	}
	if prepared.Provider == nil {
		return nil, errors.New("run view provider evidence is unavailable")
	}
	completion, err := prepared.Adapter.Run(ctx, viewRequest, nil)
	if err != nil {
		return nil, err
	}
	if completion.NativeSessionID == nil || strings.TrimSpace(*completion.NativeSessionID) == "" {
		return nil, errors.New("run view provider returned no native session reference")
	}
	result := &adapter.RunView{
		Policy:          view.Policy,
		Provider:        *prepared.Provider,
		NativeSessionID: *completion.NativeSessionID,
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}

func stringRaw(value string) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

var _ adapter.RunViewGateway = RunViewGateway{}
