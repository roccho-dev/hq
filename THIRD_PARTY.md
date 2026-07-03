# Third party modules

This POC keeps dependencies as Go modules and uses local `replace` directives for offline reproducibility.

- `github.com/reeflective/readline` is included under `third_party/readline` from the supplied `readline-master.zip`.
- `github.com/rivo/uniseg` is represented by a minimal local module under `deps/github.com/rivo/uniseg`.
- `golang.org/x/sys` is represented by a minimal local module under `deps/golang.org/x/sys` with Linux and Windows build surfaces used by this POC.

For production, prefer upstream modules by removing the two low-level dependency replacements if network/module cache is available.
