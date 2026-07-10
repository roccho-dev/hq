// hq-worker-fixture is an evidence-only cross-platform child process used by
// direct-executable adapter tests and built-binary proof.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	stdout := flag.String("stdout", "", "text written to stdout")
	stderr := flag.String("stderr", "", "text written to stderr")
	argsFile := flag.String("args-file", "", "optional JSON file receiving positional arguments")
	envKey := flag.String("env-key", "", "optional environment key to inspect")
	envFile := flag.String("env-file", "", "optional file receiving the inspected environment value")
	sentinel := flag.String("sentinel", "", "optional file appended once when the process starts")
	sleep := flag.Duration("sleep", 0, "optional delay before exit")
	exitCode := flag.Int("exit-code", 0, "process exit code")
	flag.Parse()

	if *sentinel != "" {
		file, err := os.OpenFile(*sentinel, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			fatal(err)
		}
		if _, err := file.WriteString("started\n"); err != nil {
			_ = file.Close()
			fatal(err)
		}
		if err := file.Close(); err != nil {
			fatal(err)
		}
	}
	if *argsFile != "" {
		encoded, err := json.Marshal(flag.Args())
		if err != nil {
			fatal(err)
		}
		encoded = append(encoded, '\n')
		if err := os.WriteFile(*argsFile, encoded, 0o600); err != nil {
			fatal(err)
		}
	}
	if *envFile != "" {
		if err := os.WriteFile(*envFile, []byte(os.Getenv(*envKey)), 0o600); err != nil {
			fatal(err)
		}
	}

	_, _ = fmt.Fprint(os.Stdout, *stdout)
	_, _ = fmt.Fprint(os.Stderr, *stderr)
	if *sleep > 0 {
		time.Sleep(*sleep)
	}
	if *exitCode < 0 || *exitCode > 255 {
		fatal(fmt.Errorf("exit-code must be between 0 and 255"))
	}
	os.Exit(*exitCode)
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "fixture error:", err)
	os.Exit(125)
}
