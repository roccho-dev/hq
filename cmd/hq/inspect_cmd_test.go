package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hq/internal/hqprofile"
)

const inspectWorld = `{"kind":"hq.world.v1","world_id":"world.inspect"}
{"kind":"hq.command.v1","command_id":"proof.run","command_version":"1","name":"proof.run","instruction":{"version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["proof"]}}}
`

func TestWorldAndProfileInspectEmitExactStrictSelectionWithoutWrites(t *testing.T) {
	root := t.TempDir()
	profileRoot, profile, worldPath := writeInspectProfile(t, root, inspectWorld)
	beforeAccepted := mustRead(t, profile.AcceptedPath)

	var worldOut, worldErr bytes.Buffer
	matched, code := runSubcommand([]string{"world", "inspect", "--path", worldPath, "--json"}, strings.NewReader("ignored query"), &worldOut, &worldErr)
	if !matched || code != 0 || worldErr.Len() != 0 {
		t.Fatalf("world inspect matched=%v code=%d stdout=%q stderr=%q", matched, code, worldOut.String(), worldErr.String())
	}
	var worldReport selectedWorldInspection
	if err := json.Unmarshal(bytes.TrimSpace(worldOut.Bytes()), &worldReport); err != nil {
		t.Fatal(err)
	}
	if worldReport.Kind != selectedWorldInspectionKind || worldReport.World.WorldID != "world.inspect" || worldReport.World.Digest == "" {
		t.Fatalf("world report=%+v", worldReport)
	}
	if strings.Count(worldOut.String(), "\n") != 1 {
		t.Fatalf("world inspection must emit exactly one JSON line: %q", worldOut.String())
	}

	var profileOut, profileErr bytes.Buffer
	matched, code = runSubcommand([]string{"profile", "inspect", "--profile", profile.Name, "--profile-root", profileRoot, "--json"}, strings.NewReader("ignored query"), &profileOut, &profileErr)
	if !matched || code != 0 || profileErr.Len() != 0 {
		t.Fatalf("profile inspect matched=%v code=%d stdout=%q stderr=%q", matched, code, profileOut.String(), profileErr.String())
	}
	var profileReport profileSelectionInspection
	if err := json.Unmarshal(bytes.TrimSpace(profileOut.Bytes()), &profileReport); err != nil {
		t.Fatal(err)
	}
	if profileReport.Kind != profileSelectionInspectionKind || profileReport.Profile != profile.Name || profileReport.DeploymentID != profile.DeploymentID || profileReport.WorldPath != worldPath || profileReport.World != worldReport.World {
		t.Fatalf("profile report=%+v world=%+v", profileReport, worldReport)
	}
	if strings.Count(profileOut.String(), "\n") != 1 {
		t.Fatalf("profile inspection must emit exactly one JSON line: %q", profileOut.String())
	}
	if after := mustRead(t, profile.AcceptedPath); !bytes.Equal(beforeAccepted, after) {
		t.Fatal("inspection changed accepted ledger")
	}
	if _, err := os.Stat(profile.EventsPath); !os.IsNotExist(err) {
		t.Fatalf("inspection created event/history evidence: %v", err)
	}
}

func TestProfileInspectionMatchesFreshLSPInitializeSelection(t *testing.T) {
	root := t.TempDir()
	profileRoot, profile, _ := writeInspectProfile(t, root, inspectWorld)
	beforeAccepted := mustRead(t, profile.AcceptedPath)

	var profileOut, profileErr bytes.Buffer
	if matched, code := runSubcommand([]string{"profile", "inspect", "--profile", profile.Name, "--profile-root", profileRoot, "--json"}, strings.NewReader(""), &profileOut, &profileErr); !matched || code != 0 {
		t.Fatalf("profile inspect matched=%v code=%d stderr=%q", matched, code, profileErr.String())
	}
	var inspected profileSelectionInspection
	if err := json.Unmarshal(bytes.TrimSpace(profileOut.Bytes()), &inspected); err != nil {
		t.Fatal(err)
	}

	initialize := lspFrame(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	exit := lspFrame(t, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	var lspOut, lspErr bytes.Buffer
	matched, code := runSubcommand([]string{"lsp", "--profile", profile.Name, "--profile-root", profileRoot}, strings.NewReader(initialize+exit), &lspOut, &lspErr)
	if !matched || code != 0 || lspErr.Len() != 0 {
		t.Fatalf("lsp matched=%v code=%d stdout=%q stderr=%q", matched, code, lspOut.String(), lspErr.String())
	}
	var response struct {
		Result struct {
			Capabilities struct {
				Experimental struct {
					HQ struct {
						Kind         string `json:"kind"`
						Runtime      string `json:"runtime"`
						Profile      string `json:"profile"`
						DeploymentID string `json:"deployment_id"`
						World        struct {
							WorldID string `json:"world_id"`
							Digest  string `json:"digest"`
						} `json:"world"`
					} `json:"hq"`
				} `json:"experimental"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(lspBody(t, lspOut.Bytes()), &response); err != nil {
		t.Fatal(err)
	}
	hq := response.Result.Capabilities.Experimental.HQ
	if hq.Kind != "hq.runtimeSelection.v1" || hq.Runtime != "lsp" || hq.Profile != inspected.Profile || hq.DeploymentID != inspected.DeploymentID || hq.World.WorldID != inspected.World.WorldID || hq.World.Digest != inspected.World.Digest {
		t.Fatalf("initialize hq=%+v inspected=%+v", hq, inspected)
	}
	if after := mustRead(t, profile.AcceptedPath); !bytes.Equal(beforeAccepted, after) {
		t.Fatal("initialize changed accepted ledger")
	}
	if _, err := os.Stat(profile.EventsPath); !os.IsNotExist(err) {
		t.Fatalf("initialize created event/history evidence: %v", err)
	}
}

func TestWorldInspectFailuresEmitNoSuccessRecord(t *testing.T) {
	for name, path := range map[string]string{
		"relative":  "world.jsonl",
		"directory": t.TempDir(),
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			matched, code := runSubcommand([]string{"world", "inspect", "--path", path, "--json"}, strings.NewReader(""), &stdout, &stderr)
			if !matched || code == 0 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("matched=%v code=%d stdout=%q stderr=%q", matched, code, stdout.String(), stderr.String())
			}
		})
	}
	for name, data := range map[string]string{
		"malformed":     inspectWorld + "{",
		"identity-free": strings.SplitN(inspectWorld, "\n", 2)[1],
		"duplicate":     strings.SplitN(inspectWorld, "\n", 2)[0] + "\n" + inspectWorld,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "world.jsonl")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			_, code := runSubcommand([]string{"world", "inspect", "--path", path, "--json"}, strings.NewReader(""), &stdout, &stderr)
			if code == 0 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestInspectRequiresExplicitJSONAndAbsoluteProfileRoot(t *testing.T) {
	for _, args := range [][]string{
		{"world", "inspect", "--path", filepath.Join(t.TempDir(), "world.jsonl")},
		{"profile", "inspect", "--profile", "local", "--profile-root", "relative", "--json"},
	} {
		var stdout, stderr bytes.Buffer
		matched, code := runSubcommand(args, strings.NewReader(""), &stdout, &stderr)
		if !matched || code == 0 || stdout.Len() != 0 {
			t.Fatalf("args=%v matched=%v code=%d stdout=%q stderr=%q", args, matched, code, stdout.String(), stderr.String())
		}
	}
}

func writeInspectProfile(t *testing.T, root, worldData string) (string, hqprofile.Profile, string) {
	t.Helper()
	workspace := filepath.Join(root, "workspace")
	eventsDir := filepath.Join(workspace, ".hq", "events")
	profileRoot := filepath.Join(root, "profiles")
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(profileRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(root, "world.jsonl")
	acceptedPath := filepath.Join(root, "accepted.jsonl")
	if err := os.WriteFile(worldPath, []byte(worldData), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(acceptedPath, []byte("sentinel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{
		Kind: hqprofile.Kind, Name: "local", DeploymentID: "deployment-1",
		WorldPath: worldPath, AcceptedPath: acceptedPath, WorkspaceRoot: workspace,
		EventsPath: filepath.Join(eventsDir, "events.jsonl"), PollIntervalMS: 50, HealthTimeoutMS: 500,
	}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileRoot, profile.Name+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return profileRoot, profile, worldPath
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func lspFrame(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
}

func lspBody(t *testing.T, wire []byte) []byte {
	t.Helper()
	parts := bytes.SplitN(wire, []byte("\r\n\r\n"), 2)
	if len(parts) != 2 {
		t.Fatalf("invalid LSP response framing: %q", wire)
	}
	return parts[1]
}
