package hq

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type forbiddenCompilerPattern struct {
	needle string
	reason string
}

var forbiddenCompilerPatterns = []forbiddenCompilerPattern{
	{needle: `"os/exec"`, reason: "process-launch import"},
	{needle: `"golang.org/x/sys/execabs"`, reason: "process-launch import"},
	{needle: `exec.Command(`, reason: "process launch"},
	{needle: `exec.CommandContext(`, reason: "process launch"},
	{needle: `execabs.Command(`, reason: "process launch"},
	{needle: `os.StartProcess(`, reason: "process launch"},
	{needle: `syscall.StartProcess(`, reason: "process launch"},
	{needle: `syscall.Exec(`, reason: "process replacement"},
	{needle: `syscall.ForkExec(`, reason: "process launch"},
	{needle: `syscall.Syscall(`, reason: "raw syscall escape"},
	{needle: `syscall.Syscall6(`, reason: "raw syscall escape"},
	{needle: `unix.Exec(`, reason: "process replacement"},
	{needle: `unix.ForkExec(`, reason: "process launch"},
	{needle: `windows.CreateProcess(`, reason: "Windows process launch"},
	{needle: `windows.CreateProcessAsUser(`, reason: "Windows process launch"},
	{needle: `windows.CreateProcessWithLogonW(`, reason: "Windows process launch"},
	{needle: `windows.CreateProcessWithTokenW(`, reason: "Windows process launch"},
	{needle: `plugin.Open(`, reason: "runtime code loading"},
	{needle: `import "C"`, reason: "cgo execution escape"},
	{needle: `C.system(`, reason: "cgo process launch"},
	{needle: `C.popen(`, reason: "cgo process launch"},
	{needle: `//go:linkname`, reason: "linkname execution escape"},
	{needle: `"github.com/creack/pty"`, reason: "PTY execution dependency"},
	{needle: `"github.com/UserExistsError/conpty"`, reason: "PTY execution dependency"},
	{needle: `"herdr"`, reason: "named execution adapter"},
	{needle: `"codex"`, reason: "named execution adapter"},
	{needle: `"claude"`, reason: "named execution adapter"},
	{needle: `"sh"`, reason: "named execution adapter"},
	{needle: `"pwsh"`, reason: "named execution adapter"},
	{needle: `"powershell"`, reason: "named execution adapter"},
	{needle: `"cmd.exe"`, reason: "named execution adapter"},
	{needle: `"/bin/sh"`, reason: "named execution adapter"},
}

func TestCompilerPackagesHaveNoExecutionOrAdapterCreep(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	roots := []string{
		filepath.Join(repoRoot, "internal", "core"),
		filepath.Join(repoRoot, "internal", "hq"),
		filepath.Join(repoRoot, "cmd", "hq"),
	}

	var violations []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, violation := range executionBoundaryViolations(path, string(source)) {
				violations = append(violations, violation)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("hq compiler boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestExecutionBoundaryGuardRejectsProcessAndAdapterCreep(t *testing.T) {
	fixtures := map[string]string{
		"process import":         "package fixture\nimport \"os/exec\"\n",
		"process call":           "package fixture\nfunc run() { os.StartProcess(\"tool\", nil, nil) }\n",
		"windows process call":   "package fixture\nfunc run() { windows.CreateProcess(nil, nil, nil, nil, false, 0, nil, nil, nil, nil) }\n",
		"raw syscall":            "package fixture\nfunc run() { syscall.Syscall(0, 0, 0, 0) }\n",
		"cgo process call":       "package fixture\nimport \"C\"\nfunc run() { C.system(nil) }\n",
		"linkname escape":        "package fixture\n//go:linkname run hidden.run\nfunc run()\n",
		"named adapter":          "package fixture\nconst target = \"Codex\"\n",
		"pty dependency":         "package fixture\nimport \"github.com/creack/pty\"\n",
		"shell replacement":      "package fixture\nfunc run() { syscall.Exec(\"/bin/sh\", nil, nil) }\n",
		"runtime plugin loading": "package fixture\nfunc run() { plugin.Open(\"adapter.so\") }\n",
	}

	for name, source := range fixtures {
		t.Run(name, func(t *testing.T) {
			if got := executionBoundaryViolations("fixture.go", source); len(got) == 0 {
				t.Fatalf("guard accepted forbidden fixture: %s", source)
			}
		})
	}
}

func executionBoundaryViolations(path, source string) []string {
	normalizedSource := strings.ToLower(source)
	var violations []string
	for _, pattern := range forbiddenCompilerPatterns {
		if strings.Contains(normalizedSource, strings.ToLower(pattern.needle)) {
			violations = append(violations, fmt.Sprintf("%s: %s %q", path, pattern.reason, pattern.needle))
		}
	}
	return violations
}
