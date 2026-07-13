module hq

go 1.23.0

require (
	github.com/reeflective/readline v0.0.0
	github.com/rivo/uniseg v0.4.7
	github.com/sahilm/fuzzy v0.1.3
	golang.org/x/sys v0.45.0
)

replace github.com/reeflective/readline => ./third_party/readline
replace github.com/rivo/uniseg => ./deps/github.com/rivo/uniseg
replace github.com/sahilm/fuzzy => ./deps/github.com/sahilm/fuzzy
replace golang.org/x/sys => ./deps/golang.org/x/sys
