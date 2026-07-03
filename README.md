# hq-reflective-poc

`reeflective/readline` を使った、JSONL-aware autocomplete compiler の実装済み POC です。

中心は REPL ではなく、次の protocol です。

```text
JsonlWorld + CursorContext -> Suggestion[] -> compileDraft -> accepted instruction JSONL
```

## できること

- Linux / Windows 共通ビルド
- vi insert / command mode
- Tab completion
- as-you-type autocomplete
- history autosuggest
- candidate detail / description / tag 表示
- `Ctrl-T` で現在の top candidate / line の `compileDraft` preview
- `Enter` で accepted instruction を JSONL queue へ append
- 非対話 `--complete`, `--context`, `--draft` による core 実証

## 外部依存

外部依存は Go module として扱っています。

```text
github.com/reeflective/readline v0.0.0 => ./third_party/readline
github.com/rivo/uniseg        v0.4.7 => ./deps/github.com/rivo/uniseg
golang.org/x/sys             v0.45.0 => ./deps/golang.org/x/sys
```

この zip は sandbox / offline でもビルドできるように local `replace` を使っています。
ネットワークまたは module cache が使える環境では、`github.com/rivo/uniseg` と `golang.org/x/sys` の `replace` を外して上流moduleへ戻せます。

## build

```bash
go test ./...
go build -o dist/hq-reflective-linux-amd64 ./cmd/hq-reflective
GOOS=windows GOARCH=amd64 go build -o dist/hq-reflective-windows-amd64.exe ./cmd/hq-reflective
```

または:

```bash
./scripts/check.sh
```

## interactive

```bash
./dist/hq-reflective-linux-amd64 --queue instruction.jsonl
```

試す入力:

```text
{"         # Tab: required keys
{"op":q   # Tab: queue.* enum values
Ctrl-T     # compileDraft preview
ESC        # vi command mode
Enter      # accepted instruction output / queue append
```

## non-interactive proof

```bash
./dist/hq-reflective-linux-amd64 --complete '{"op":q'
./dist/hq-reflective-linux-amd64 --context '{"'
./dist/hq-reflective-linux-amd64 --draft '{"op":"queue.create","target":"ctx","payload":{"path":"demo.jsonl"}}'
```

## schema差し替え

`examples/hq.schema.jsonl` を差し替えると、Goコードを書き換えずにkey/value候補が変わります。

```bash
./dist/hq-reflective-linux-amd64 --schema examples/hq.schema.jsonl
```
