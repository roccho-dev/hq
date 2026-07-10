package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/roccho-dev/ops/packages/hq/internal/hostopen"
	"github.com/roccho-dev/ops/packages/hq/internal/profile"
	"github.com/roccho-dev/ops/packages/hq/internal/queue"
)

type fakeOpener struct {
	path  string
	calls int
}

func (o *fakeOpener) Open(path string) (hostopen.Result, error) {
	o.path = path
	o.calls++
	return hostopen.Result{Executable: "explorer.exe", Args: []string{path}, PID: 42}, nil
}

func TestRunOncePassesQueuedPathDirectlyToHostAdapter(t *testing.T) {
	root := prepareRoot(t)
	p, err := profile.Load(root, "local")
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	row, err := queue.NewRow("local", "file:///request.hqjson", 7, `{"kind":"hq.hostOpenRequest.v1"}`, queue.HostOpenRequest{Kind: queue.HostOpenRequestKind, Path: target})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Append(p.QueuePath, row); err != nil {
		t.Fatal(err)
	}
	opener := &fakeOpener{}
	result, err := RunOnce(p, opener)
	if err != nil {
		t.Fatal(err)
	}
	if opener.path != target {
		t.Fatalf("runner path = %q, want %q", opener.path, target)
	}
	if result.Receipt == nil || result.Receipt.Executable != "explorer.exe" || result.Receipt.Status != "launched" {
		t.Fatalf("unexpected receipt: %#v", result.Receipt)
	}
}

func TestRunOnceProcessesRepeatedExplicitSubmissions(t *testing.T) {
	root := prepareRoot(t)
	p, err := profile.Load(root, "local")
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	request := queue.HostOpenRequest{Kind: queue.HostOpenRequestKind, Path: target}
	for range 2 {
		row, err := queue.NewRow("local", "file:///request.hqjson", 7, `{"kind":"hq.hostOpenRequest.v1"}`, request)
		if err != nil {
			t.Fatal(err)
		}
		if err := queue.Append(p.QueuePath, row); err != nil {
			t.Fatal(err)
		}
	}
	opener := &fakeOpener{}
	for range 2 {
		result, err := RunOnce(p, opener)
		if err != nil {
			t.Fatal(err)
		}
		if result.Processed != 1 {
			t.Fatalf("processed = %d, want 1", result.Processed)
		}
	}
	if opener.calls != 2 {
		t.Fatalf("open calls = %d, want 2", opener.calls)
	}
}

func prepareRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{filepath.Join(root, "profiles", "local"), filepath.Join(root, "queues"), filepath.Join(root, "receipts")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	catalog := `{"kind":"hq.commandSpec.v1","id":"host.open","label":"explorer.open","detail":"open","insertText":"{}","bufferKind":"hq.hostOpenRequest.v1"}` + "\n"
	files := map[string]string{
		filepath.Join(root, "profiles", "local", "catalog.jsonl"):        catalog,
		filepath.Join(root, "queues", "hq.host-command.queue.jsonl"):     "",
		filepath.Join(root, "receipts", "hq.host-command.receipt.jsonl"): "",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
