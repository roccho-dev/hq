# Third party modules

Dependencies are Go modules. Local `replace` directives keep the official `hq` runtime buildable in offline or sandboxed proof environments.

- `github.com/reeflective/readline` is included under `third_party/readline` from the supplied `readline-master.zip`.
- `github.com/rivo/uniseg` is represented by a minimal local module under `deps/github.com/rivo/uniseg`.
- `github.com/sahilm/fuzzy v0.1.3` (origin commit
  `8aadc77d46cf09f5dc529d41358a94a24516c0b6`) supplies only the Unicode-aware
  `FindNoSort` subsequence primitive used by world recall. Its upstream
  `fuzzy.go` is reproduced byte-for-byte under `deps/github.com/sahilm/fuzzy`.
  The adjacent local `go.mod` is repository compatibility metadata for the Go
  1.23/offline replacement; it is not claimed to be an unmodified module tree.
  The upstream module and module-file checksums remain pinned in `go.sum`, and
  the MIT license is reproduced in `third_party/licenses/sahilm-fuzzy-LICENSE`.
- `golang.org/x/sys` is represented by a minimal local module under `deps/golang.org/x/sys` with Linux and Windows build surfaces used by `hq`.

For a networked production build, prefer upstream modules by removing the low-level dependency replacements when module cache or internet access is available.
