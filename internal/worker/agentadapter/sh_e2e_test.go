package agentadapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShRealBinaryLiteralInjection(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	literal := "; touch " + marker + " && echo $(uname)"
	completion, err := (Sh{}).Run(context.Background(), req("sh", ShPayload{Argv: []string{executable, "-test.run=TestShHelperProcess", "--", "echo", literal}}, "."), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(completion.FinalText, literal) {
		t.Fatalf("literal argv was not preserved: %q", completion.FinalText)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shell metacharacters caused a side effect: %v", err)
	}
}

func TestShRealBinaryTimeoutAndCancel(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	request := req("sh", ShPayload{Argv: []string{executable, "-test.run=TestShHelperProcess", "--", "sleep"}}, ".")

	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		started := time.Now()
		_, err := (Sh{}).Run(ctx, request, nil)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error=%v", err)
		}
		if time.Since(started) > 3*time.Second {
			t.Fatal("timeout did not interrupt the subprocess promptly")
		}
	})

	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()
		started := time.Now()
		_, err := (Sh{}).Run(ctx, request, nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v", err)
		}
		if time.Since(started) > 3*time.Second {
			t.Fatal("cancel did not interrupt the subprocess promptly")
		}
	})
}

func TestShHelperProcess(t *testing.T) {
	separator := -1
	for index, value := range os.Args {
		if value == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	switch os.Args[separator+1] {
	case "echo":
		if separator+2 < len(os.Args) {
			fmt.Print(os.Args[separator+2])
		}
	case "sleep":
		time.Sleep(30 * time.Second)
	}
}
