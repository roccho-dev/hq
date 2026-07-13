package selectedworld

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const worldManifest = `{"kind":"hq.world.v1","world_id":"world.observation-proof"}`
const worldCommand = `{"kind":"hq.command.v1","command_id":"proof.run","command_version":"1","name":"proof.run","instruction":{"version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["proof"]}}}`
const worldKey = `{"key":"reason","type":"string","required":true,"description":"proof"}`

func TestLoadIdentityIsSemanticAndPathIndependent(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.jsonl")
	secondDir := filepath.Join(root, "elsewhere")
	if err := os.Mkdir(secondDir, 0o700); err != nil {
		t.Fatal(err)
	}
	secondPath := filepath.Join(secondDir, "renamed.jsonl")
	first := strings.Join([]string{worldManifest, worldCommand, worldKey}, "\n") + "\n"
	second := strings.Join([]string{
		`{ "description" : "proof", "required" : true, "type" : "string", "key" : "reason" }`,
		`{"world_id":"world.observation-proof","kind":"hq.world.v1"}`,
		`{"instruction":{"target":"sh","payload":{"argv":["proof"]},"op":"run","version":"instruction.v1"},"name":"proof.run","command_version":"1","command_id":"proof.run","kind":"hq.command.v1"}`,
	}, "\r\n") + "\r\n"
	if err := os.WriteFile(firstPath, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	left, err := Load(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	right, err := Load(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if left.Ref != right.Ref {
		t.Fatalf("semantic identity changed: left=%+v right=%+v", left.Ref, right.Ref)
	}
	const crossPlatformDigest = "sha256:1fffb34a8f81ef70308a0e707e479c02730937446f7ffbcca81b42ff6a6fb342"
	if left.Ref.WorldID != "world.observation-proof" || left.Ref.Digest != crossPlatformDigest {
		t.Fatalf("cross-platform identity=%+v", left.Ref)
	}
}

func TestLoadRejectsNonAbsoluteNonRegularAndInvalidSelectedWorlds(t *testing.T) {
	if _, err := Load("relative.jsonl"); err == nil {
		t.Fatal("relative path was accepted")
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("directory path was accepted")
	}
	symlinkRoot := t.TempDir()
	target := filepath.Join(symlinkRoot, "target.jsonl")
	link := filepath.Join(symlinkRoot, "link.jsonl")
	if err := os.WriteFile(target, []byte(worldManifest+"\n"+worldCommand+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Logf("symlink proof unavailable on this host: %v", err)
	} else if _, err := Load(link); err == nil {
		t.Fatal("symlink selected-world path was accepted")
	}
	for name, data := range map[string]string{
		"malformed":          worldManifest + "\n{",
		"identity-free":      worldCommand + "\n",
		"duplicate identity": worldManifest + "\n" + worldManifest + "\n" + worldCommand + "\n",
		"contradictory command": worldManifest + "\n" + worldCommand + "\n" +
			strings.Replace(worldCommand, `"name":"proof.run"`, `"name":"other.run"`, 1) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "world.jsonl")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatalf("invalid selected world was accepted: %s", data)
			}
		})
	}
}
