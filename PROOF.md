# Proof

Executed in the sandbox:

```text
$ go test ./...
?   	hq-reflective-poc/cmd/hq-reflective	[no test files]
ok  	hq-reflective-poc/internal/hq

$ go build -o dist/hq-reflective-linux-amd64 ./cmd/hq-reflective
$ GOOS=windows GOARCH=amd64 go build -o dist/hq-reflective-windows-amd64.exe ./cmd/hq-reflective

$ go list -m all
hq-reflective-poc
github.com/reeflective/readline v0.0.0 => ./third_party/readline
github.com/rivo/uniseg v0.4.7 => ./deps/github.com/rivo/uniseg
golang.org/x/sys v0.45.0 => ./deps/golang.org/x/sys
```

Behavior proof:

```bash
./dist/hq-reflective-linux-amd64 --complete '{"op":q'
```

returns candidates whose `compileDraft` contains `queue`, `key`, `value`, `instruction`, and `reason`.

Previous terminal screenshots are included under `docs/screens/`.
