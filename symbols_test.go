package pkgsyms_test

import (
	"reflect"
	"testing"

	"github.com/skillian/pkgsyms"
)

func TestDynamic(t *testing.T) {
	p := pkgsyms.Of("github.com/skillian/pkgsyms")
	tp, err := p.Lookup("Package")
	if err != nil {
		t.Fatal(err)
	}
	v, ok := reflect.New(tp.(pkgsyms.Type).Type()).Interface().(*pkgsyms.Package)
	if !ok {
		t.Fatalf("expected %T but got %T", (*pkgsyms.Package)(nil), v)
	}
}
