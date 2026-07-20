package localtool

import (
	"context"
	"testing"

	"hq/internal/core"
)

func TestRunViewGatewayOpensExactFiniteProvider(t *testing.T) {
	executable, registryPath := executableBindingFixture(t, "local-tool.main", "1", "app.main")
	registry := VerifiedBindings{
		Schema: VerifiedBindingsSchema,
		Entries: []VerifiedBinding{
			{
				BindingRef: "local-tool.main", ResourceID: "app.main", ContractVersion: "1", Executable: executable,
				MaterialDigest: sha256File(t, executable), DeploymentID: "app.main@1:" + sha256File(t, executable),
				DeclarationEventID: "decl-main", SelectionEventID: "select-main",
			},
			{
				BindingRef: "local-tool.viewer", ResourceID: "app.viewer", ContractVersion: "1", Executable: executable,
				MaterialDigest: sha256File(t, executable), DeploymentID: "app.viewer@1:" + sha256File(t, executable),
				DeclarationEventID: "decl-viewer", SelectionEventID: "select-viewer",
			},
		},
	}
	writeRegistry(t, registryPath, registry)

	mainTool := helperTool("echo", "text")
	mainTool.BindingRef = "local-tool.main"
	mainTool.Actions[0].RunView = &core.LocalToolRunView{
		Policy: "required", ToolID: "viewer", ToolVersion: "1", ActionID: "open",
	}
	viewer := helperTool("json", "json")
	viewer.ToolID = "viewer"
	viewer.BindingRef = "local-tool.viewer"
	viewer.Actions[0].ActionID = "open"
	viewer.Actions[0].Inputs = []core.LocalToolInput{
		{Name: "view_id", Type: "string", Required: true},
		{Name: "run_id", Type: "string", Required: true},
		{Name: "events_path", Type: "string", Required: true},
	}
	viewer.Actions[0].NativeRefs.Session = &core.LocalToolNativeSelector{Source: "stdout", Path: []string{"session", "id"}}

	world := &core.JsonlWorld{LocalTools: []core.LocalToolDefinition{mainTool, viewer}}
	preparer := Preparer{World: world, BindingsPath: registryPath}
	request := requestFor(mainTool, nil)
	prepared, err := preparer.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.RunViewRequired {
		t.Fatal("required run view was not preserved in the prepared contract")
	}
	view, err := (RunViewGateway{Preparer: preparer, EventsPath: t.TempDir() + "/events.jsonl"}).Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if view == nil || view.Policy != "required" || view.NativeSessionID != "session-1" || view.Provider.ProviderID != "local-tool.viewer" {
		t.Fatalf("view=%+v", view)
	}
}

func TestRunViewGatewayRejectsRecursiveView(t *testing.T) {
	_, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	tool := helperTool("json", "json")
	tool.Actions[0].RunView = &core.LocalToolRunView{
		Policy: "required", ToolID: tool.ToolID, ToolVersion: tool.ToolVersion, ActionID: tool.Actions[0].ActionID,
	}
	world := &core.JsonlWorld{LocalTools: []core.LocalToolDefinition{tool}}
	request := requestFor(tool, map[string]any{})
	if _, err := (RunViewGateway{
		Preparer:   Preparer{World: world, BindingsPath: registryPath},
		EventsPath: t.TempDir() + "/events.jsonl",
	}).Open(context.Background(), request); err == nil {
		t.Fatal("recursive run view was accepted")
	}
}
