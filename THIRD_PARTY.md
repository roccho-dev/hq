# Third party modules

Dependencies are Go modules. Local `replace` directives keep the official `hq` runtime buildable in offline or sandboxed proof environments.

- `github.com/reeflective/readline` is included under `third_party/readline` from the supplied `readline-master.zip`.
- `github.com/rivo/uniseg` is represented by a minimal local module under `deps/github.com/rivo/uniseg`.
- `golang.org/x/sys` is represented by a minimal local module under `deps/golang.org/x/sys` with Linux and Windows build surfaces used by `hq`.

For a networked production build, prefer upstream modules by removing the low-level dependency replacements when module cache or internet access is available.
