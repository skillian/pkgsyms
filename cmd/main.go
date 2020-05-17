package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/skillian/errors"
	"golang.org/x/tools/go/packages"
)

const (
	pkgsyms       = "pkgsyms"
	progGoPkgPath = "github.com/skillian/" + pkgsyms
	pkgsymsGo     = pkgsyms + ".go"

	pkgNeeds = (packages.NeedName |
		packages.NeedFiles |
		packages.NeedCompiledGoFiles |
		packages.NeedImports |
		packages.NeedTypes | packages.NeedTypesSizes |
		packages.NeedSyntax | packages.NeedTypesInfo |
		packages.NeedDeps)
)

var (
	progname = filepath.Base(os.Args[0])

	output  = flag.String("output", "", "output filename; default srcdir/"+pkgsymsGo)
	varname = flag.String("varname", "Pkg", "variable name of the package symbols")
	pkgname = flag.String("package", "", "package name to use in the output")
	//pkgprefix = flag.String("prefix", "", "the package prefix")
	srcdir string
)

func usage() {
	fmt.Fprintf(os.Stderr, `Create a plugin-like object to access symbols from a package.

Usage of %s:
	%s [flags] [directory]

The directory must be a Go package.

Flags:
`, progname, progname)
	flag.PrintDefaults()
}

func main() {
	var err error
	log.SetFlags(0)
	log.SetPrefix("pkg: ")
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	switch len(args) {
	case 0:
		if srcdir, err = os.Getwd(); err != nil {
			log.Fatal(errors.ErrorfWithCause(
				err, "failed to get current working directory"))
		}
		// Getwd can return one of multiple possibly symlinked paths,
		// so let's clean up and (try to) keep it consistent!
		srcdir = filepath.Clean(srcdir)
	case 1:
		srcdir = args[0]
	default:
		log.Fatal(errors.Errorf(
			"only one package directory is allowed."))
	}
	// if len(strings.TrimSpace(*output)) == 0 {
	// 	*output = filepath.Join(srcdir, pkgsymsGo)
	// }

	outfile, err := getOutput()
	if err != nil {
		log.Fatal(errors.ErrorfWithCause(
			err, "failed to get output file: %q", *output))
	}
	defer outfile.Close()
	g := generator{
		pkg:   mustParsePackage(srcdir),
		decls: make([]decl, 0, 512),
	}
	g.generate()

	declstrs := make([]string, len(g.decls))
	for i, d := range g.decls {
		declstrs[i] = strings.Join(
			[]string{"\t\t", d.String(), ",\n"}, "")
	}

	fmt.Fprintf(
		outfile, `// Code generated by "%s %s"; DO NOT EDIT.

package %s

import %q

var %s = %s.Of(%q)

func init() {
	%s.Add(
%s	)
}
`,
		progname, strings.Join(os.Args[1:], " "),
		*pkgname,
		progGoPkgPath,
		*varname, pkgsyms, srcdir,
		*varname,
		strings.Join(declstrs, ""),
	)
}

func mustParsePackage(srcdir string) *packages.Package {
	pkg, err := parsePackage(srcdir)
	if err != nil {
		log.Fatal(err)
	}
	return pkg
}

func parsePackage(srcdir string) (*packages.Package, error) {
	cfg := packages.Config{Mode: pkgNeeds}
	pkgs, err := packages.Load(&cfg, srcdir)
	if err != nil {
		return nil, errors.ErrorfWithCause(
			err, "failed to parse %q", srcdir)
	}
	if len(pkgs) != 1 {
		return nil, errors.Errorf(
			"expected exactly one package when parsing %q, not %d",
			srcdir, len(pkgs))
	}
	return pkgs[0], nil
}

type generator struct {
	pkg    *packages.Package
	decls  []decl
	prefix string
}

func (g *generator) generate() {
	if g.pkg.Name == *pkgname {
		g.prefix = ""
	} else {
		g.prefix = g.pkg.Name + "."
	}
	for _, f := range g.pkg.Syntax {
		ast.Inspect(f, g.inspect)
	}
}

func (g *generator) inspect(n ast.Node) bool {
	kind := ""
	var sb strings.Builder
	switch n := n.(type) {
	case *ast.GenDecl:
		switch n.Tok {
		case token.TYPE:
			for _, s := range n.Specs {
				name := s.(*ast.TypeSpec).Name
				if !name.IsExported() {
					continue
				}
				g.decls = append(g.decls, decl{g: g, Kind: "Type", Name: name.Name})
			}
			return false
		case token.CONST:
			kind = "Const"
			fallthrough
		case token.VAR:
			if kind == "" {
				kind = "Var"
			}
			for _, s := range n.Specs {
				vs := s.(*ast.ValueSpec)
				for i, id := range vs.Names {
					if !id.IsExported() {
						continue
					}
					tp := vs.Type
					if tp == nil {
						tp = vs.Values[i]
					}
					sb.Reset()
					if err := printer.Fprint(&sb, g.pkg.Fset, tp); err != nil {
						log.Fatal(errors.ErrorfWithCause(
							err, "failed to get type of %#v", vs))
					}
					g.decls = append(g.decls, decl{
						g:    g,
						Kind: kind,
						Name: id.Name,
						Type: sb.String(),
					})
				}
			}
			return false
		}
	case *ast.FuncDecl:
		if n.Recv != nil {
			return true
		}
		if !n.Name.IsExported() {
			return true
		}
		g.decls = append(g.decls, decl{g: g, Kind: "Func", Name: n.Name.Name})
		return false
	}
	return true
}

type decl struct {
	g *generator

	// Kind is "Const", "Func", "Type" or "Var"
	Kind string

	// Name of the declared object
	Name string

	// optional type of the object.
	Type string
}

func (d decl) String() string {
	switch d.Kind {
	case "Type":
		return fmt.Sprintf(
			"%s.MakeType(%q, (*%s)(nil))",
			pkgsyms, d.Name, d.g.prefix+d.Name)
	case "Var":
		return fmt.Sprintf(
			"%s.Make%s(%q, &%s)",
			pkgsyms, d.Kind, d.Name, d.g.prefix+d.Name)
	default:
		return fmt.Sprintf(
			"%s.Make%s(%q, %s)",
			pkgsyms, d.Kind, d.Name, d.g.prefix+d.Name)
	}
}

func getOutput() (io.WriteCloser, error) {
	if len(*output) == 0 || *output == "-" {
		return nopCloser{os.Stdout}, nil
	}
	return os.Create(*output)
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }