// Package pkgsyms extends the reflect package so that any symbols exported by a
// package are accessible by name to another package.
//
// Note that the inability to get a type or global function by its name is not
// an oversight of the reflect package- it's a strength that allows the compiler
// and runtime to omit functions, and possibly types, that aren't referenced
// from anywhere during compilation.
//
// In order for the pkgsyms package to work, users of the pkgsyms package must
// run a go generate command that essentially subverts the compiler's ability to
// eliminate dead code by making all exported names accessible outside of
// the package.
package pkgsyms

import (
	"reflect"
	"sync"

	"github.com/skillian/errors"
)

var idx sync.Map

// Package defines a package.  It includes the package name and its exported
// symbols.
type Package struct {
	// Name of the package
	Name string

	// Symbols exported by the package
	Symbols
}

// Of gets the Package definition of the package with the given name.
func Of(name string) *Package {
	v, loaded := idx.Load(name)
	if loaded {
		return v.(*Package)
	}
	pkg := &Package{Name: name}
	v, loaded = idx.LoadOrStore(name, pkg)
	if loaded {
		return v.(*Package)
	}
	return pkg
}

// Symbol is an exported constant, function, type or variable.
type Symbol interface {
	// Name of the symbol
	Name() string

	// Get the value associated with the symbol
	Get() interface{}
}

// Symbols are exported names in a package which can include things like
// constants, functions, types and variables.
type Symbols struct {
	mutex sync.Mutex
	names map[string]int
	slice []Symbol
}

// MakeSymbols creates a collection of symbols
func MakeSymbols(capacity int) Symbols {
	if capacity < 0 {
		return Symbols{names: make(map[string]int)}
	}
	return Symbols{
		names: make(map[string]int, capacity),
		slice: make([]Symbol, 0, capacity),
	}
}

// Lookup a symbol in the set.
func (syms *Symbols) Lookup(name string) (Symbol, error) {
	syms.mutex.Lock()
	defer syms.mutex.Unlock()
	i, ok := syms.names[name]
	if !ok {
		return nil, errors.Errorf("symbol %q not found", name)
	}
	return syms.slice[i], nil
}

// Add zero or more symbols to the set.  Symbols are only added if they haven't
// already been defined.
func (syms *Symbols) Add(ss ...Symbol) {
	syms.mutex.Lock()
	defer syms.mutex.Unlock()
	if syms.names == nil {
		syms.names = make(map[string]int, cap(ss))
		syms.slice = make([]Symbol, 0, cap(ss))
	}
	for _, s := range ss {
		if _, ok := syms.names[s.Name()]; ok {
			continue
		}
		syms.names[s.Name()] = len(syms.slice)
		syms.slice = append(syms.slice, s)
	}
}

// Const holds the value of a constant.  Unlike Go compile-time constants,
// because we're actually holding onto values at runtime, these "constants"
// have actual types.
type Const struct {
	name  string
	value interface{}
}

// MakeConst creates a Const
func MakeConst(name string, value interface{}) Const {
	return Const{name: name, value: value}
}

// Name of the Constant
func (c Const) Name() string { return c.name }

// Get the value of the constant.
func (c Const) Get() interface{} { return c.value }

// Func wraps a function.
type Func struct {
	name string
	fval interface{}
}

// MakeFunc creates a Func
func MakeFunc(name string, fval interface{}) Func {
	return Func{name: name, fval: fval}
}

// Name of the function
func (f Func) Name() string { return f.name }

// Get the function value
func (f Func) Get() interface{} { return f.fval }

// Type holds a reflect.Type defined in the package.
type Type struct {
	name string
	rtyp reflect.Type
}

// MakeType creates a Type from a pointer to a value of the proper type.  For
// example:
//
// 	MakeType("MyInterface", (*MyInterface)(nil))
//
func MakeType(name string, pval interface{}) Type {
	return Type{
		name: name,
		rtyp: reflect.TypeOf(pval).Elem(),
	}
}

// Name of the type
func (t Type) Name() string { return t.name }

// Get the reflect.Type wrapped by this type.
func (t Type) Get() interface{} { return t.rtyp }

// Type is like Get, but keeps it as a reflect.Type.
func (t Type) Type() reflect.Type { return t.rtyp }

// Var is a Symbol that wraps a variable.
type Var struct {
	name string

	// addr is a pointer to the variable.
	addr interface{}

	fget func() interface{}
	fset func(v interface{})
}

// Get the value of the variable
func (v Var) Get() interface{} {
	return reflect.ValueOf(v.addr).Elem().Interface()
}

// Set the value of the variable.
func (v Var) Set(val interface{}) {
	reflect.ValueOf(v.addr).Elem().Set(reflect.ValueOf(val))
}
