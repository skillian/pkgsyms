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
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/skillian/errors"
	"golang.org/x/tools/go/packages"
)

const (
	pkgNeeds = (packages.NeedName |
		packages.NeedFiles |
		packages.NeedCompiledGoFiles |
		packages.NeedImports |
		packages.NeedTypes | packages.NeedTypesSizes |
		packages.NeedSyntax | packages.NeedTypesInfo |
		packages.NeedDeps)

	pkgsymsPkgName = "pkgsyms"
	pkgsymsPkgPath = "github.com/skillian/" + pkgsymsPkgName
)

var (
	progname = filepath.Base(os.Args[0])

	output  = flag.String("output", "", "output filename; default srcdir/pkgsyms.go")
	varname = flag.String("varname", "Pkg", "variable name of the package symbols")
	pkgname = flag.String("package", "", "package name to use in the output")
	//pkgprefix = flag.String("prefix", "", "the package prefix")
	srcdir string
)

// Config configures pkgsyms
type Config struct {
	pkgAlias string
}

// Option modifies Config.
type Option func(c *Config) error

// Alias allows an alias name to be specified for the package.
func Alias(name string) Option {
	return func(c *Config) error {
		if c.pkgAlias != "" {
			return errors.Errorf(
				"redefinition of package alias from %q to %q",
				c.pkgAlias, name)
		}
		c.pkgAlias = name
		return nil
	}
}

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
	log.SetPrefix(pkgsymsPkgName + ": ")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	switch len(args) {
	case 0:
		srcdir = "."
	case 1:
		srcdir = args[0]
	default:
		log.Fatal("one or zero directories allowed, not", len(args))
	}

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
	pkgbase := path.Base(g.pkg.Name)
	if *pkgname == "" {
		*pkgname = pkgbase
	}
	g.generate(*pkgname == pkgbase)

	sort.Slice(g.decls, func(i, j int) bool {
		a, b := g.decls[i], g.decls[j]
		c := a.kind - b.kind
		if c != 0 {
			return c < 0
		}
		return strings.Compare(a.Name, b.Name) < 0
	})

	declstrs := make([]string, len(g.decls))
	for i, d := range g.decls {
		declstrs[i] = strings.Join(
			[]string{"\t\t", d.String(), ",\n"}, "")
	}

	imports := fmt.Sprintf("%q", pkgsymsPkgPath)
	if *pkgname != pkgbase {
		imports += fmt.Sprintf("\n\t%q", g.pkg.PkgPath)
	}

	fmt.Fprintf(
		outfile, `// Code generated by "%s"; DO NOT EDIT.

package %s

import (
	%s
)

var %s = %s.Of(%q)

func init() {
	%s.Add(
%s	)
}
`,
		strings.Join(append([]string{progname}, os.Args[1:]...), " "),
		*pkgname,
		imports,
		*varname, pkgsymsPkgName, g.pkg.PkgPath,
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

func (g *generator) generate(omitPrefix bool) {
	if !omitPrefix {
		g.prefix = g.pkg.Name + "."
	}
	for _, f := range g.pkg.Syntax {
		ast.Inspect(f, g.inspect)
	}
}

func (g *generator) inspect(n ast.Node) bool {
	var kind declKind
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
				g.decls = append(g.decls, decl{g: g, kind: typeDecl, Name: name.Name})
			}
			return false
		case token.CONST:
			kind = constDecl
			fallthrough
		case token.VAR:
			if kind == badDecl {
				kind = varDecl
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
						kind: kind,
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
		g.decls = append(g.decls, decl{g: g, kind: funcDecl, Name: n.Name.Name})
		return false
	}
	return true
}

type decl struct {
	g *generator

	kind declKind

	// Name of the declared object
	Name string

	// optional type of the object.
	Type string
}

type declKind int

const (
	badDecl declKind = iota
	constDecl
	typeDecl
	funcDecl
	varDecl
)

var declStrings = []string{
	"<bad decl>",
	"Const",
	"Type",
	"Func",
	"Var",
}

func (k declKind) String() string { return declStrings[int(k)] }

func (d decl) String() string {
	switch d.kind {
	case typeDecl:
		return fmt.Sprintf(
			"%s.MakeType(%q, (*%s)(nil))",
			pkgsymsPkgName, d.Name, d.g.prefix+d.Name)
	default:
		return fmt.Sprintf(
			"%s.Make%s(%q, %s)",
			pkgsymsPkgName, d.kind, d.Name, d.g.prefix+d.Name)
	}
}

func getOutput() (io.WriteCloser, error) {
	switch {
	case *output == "-":
		return nopCloser{os.Stdout}, nil
	case len(*output) == 0:
		*output = filepath.Join(srcdir, pkgsymsPkgName+".go")
		fallthrough
	default:
		return os.Create(*output)
	}
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }
