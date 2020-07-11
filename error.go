package pkgsyms

import (
	"fmt"
	"strings"
)

type notFoundBase struct{}

func (notFoundBase) Error() string { return "symbol not found" }

// NotFound is returned when a symbol is not found in a package.
type NotFound struct {
	Pkg string
	Sym string
}

func (nf NotFound) Error() string {
	if len(nf.Pkg) > 0 {
		nf.Pkg = fmt.Sprintf("package %q: ", nf.Pkg)
	}
	if len(nf.Sym) > 0 {
		nf.Sym = fmt.Sprintf("symbol %q: ", nf.Sym)
	}
	return strings.Join([]string{nf.Pkg, nf.Sym, "not found"}, "")
}
