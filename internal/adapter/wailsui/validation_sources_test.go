package wailsui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/i18n"
)

// sourcePackage is the parsed non-test files of one directory of internal/.
type sourcePackage struct {
	fset   *token.FileSet
	files  []*ast.File
	consts map[string]ast.Expr
	funcs  map[string]*ast.FuncDecl
}

// formedValidation is one ValidationError literal of the sources and the
// values its field and reason can take.
type formedValidation struct {
	pos             string
	fields, reasons []string
}

// TestEveryFormedValidationIsTranslated reads every ValidationError{...}
// literal in the non-test sources of internal/ and checks each field and
// reason it can take against the catalogs: a new or reworded reason, or a new
// field, anywhere in internal/ fails here until the catalogs know it.
func TestEveryFormedValidationIsTranslated(t *testing.T) {
	formed := formedValidations(t, filepath.Join("..", ".."))

	seen := map[[2]string]bool{}

	for _, f := range formed {
		for _, field := range f.fields {
			for _, reason := range f.reasons {
				seen[[2]string{field, reason}] = true

				ve := &domain.ValidationError{Field: field, Reason: reason}
				ui := classify(ve)

				if _, account := accountFields[field]; !account && ui.Message != ve.Error() {
					t.Errorf("%s: message = %q, want %q", f.pos, ui.Message, ve.Error())
				}

				if ui.Messages[i18n.RU] == ui.Messages[i18n.EN] {
					t.Errorf("%s: field %q, reason %q has no Russian text", f.pos, field, reason)
				}
			}
		}
	}

	// The walk must find the literals at all: some of each kind of source.
	for _, want := range [][2]string{
		{"ssh_port", "must be between 1 and 65535"},                    // a call argument
		{"sync_phrase", "must be at least 7 characters"},               // fmt.Sprintf
		{"name", "is required"},                                        // constants
		{"google_account", "sign in required"},                         // a local variable
		{"key_file", "is an RSA key of 7 bits; at least 7 are needed"}, // another package
		{"language", "must be en or ru"},                               // this package
	} {
		if !seen[want] {
			t.Errorf("the walk did not find %q", want)
		}
	}
}

// formedValidations returns the ValidationError literals under root.
func formedValidations(t *testing.T, root string) []formedValidation {
	t.Helper()

	var out []formedValidation

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}

		pkg := parsePackage(t, path)
		for _, f := range pkg.files {
			out = append(out, pkg.validations(t, f)...)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func parsePackage(t *testing.T, dir string) *sourcePackage {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	pkg := &sourcePackage{fset: token.NewFileSet(), consts: map[string]ast.Expr{}, funcs: map[string]*ast.FuncDecl{}}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(pkg.fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}

		pkg.files = append(pkg.files, f)

		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				pkg.funcs[d.Name.Name] = d
			case *ast.GenDecl:
				if d.Tok != token.CONST {
					continue
				}

				for _, spec := range d.Specs {
					vs, _ := spec.(*ast.ValueSpec)
					for i, n := range vs.Names {
						if i < len(vs.Values) {
							pkg.consts[n.Name] = vs.Values[i]
						}
					}
				}
			}
		}
	}

	return pkg
}

// validations returns the ValidationError literals of f.
func (p *sourcePackage) validations(t *testing.T, f *ast.File) []formedValidation {
	t.Helper()

	var out []formedValidation

	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isValidationErrorType(lit.Type) {
			return true
		}

		pos := p.fset.Position(lit.Pos()).String()
		v := formedValidation{pos: pos}

		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				t.Errorf("%s: ValidationError without field names", pos)
				continue
			}

			key, _ := kv.Key.(*ast.Ident)
			values := p.values(t, pos, enclosing(f, lit), kv.Value, 0)

			switch key.Name {
			case "Field":
				v.fields = values
			case "Reason":
				v.reasons = values
			}
		}

		if len(v.fields) == 0 || len(v.reasons) == 0 {
			t.Errorf("%s: ValidationError without a field or a reason", pos)
		}

		out = append(out, v)

		return true
	})

	return out
}

// isValidationErrorType reports whether e is ValidationError or
// domain.ValidationError.
func isValidationErrorType(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name == "ValidationError"
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		return ok && pkg.Name == "domain" && x.Sel.Name == "ValidationError"
	default:
		return false
	}
}

// printfVerb is a verb of a fmt.Sprintf format.
var printfVerb = regexp.MustCompile(`%[0-9.]*[a-zA-Z]`)

// values returns the values the string expression e can take inside fn: a
// literal, a constant of the package, fmt.Sprintf with each %d a number, a
// local variable (each value assigned to it in fn) or a parameter of fn (each
// argument of the calls of fn in the package). Anything else fails the test,
// so that a new way of forming a field or a reason is noticed.
func (p *sourcePackage) values(t *testing.T, pos string, fn *ast.FuncDecl, e ast.Expr, depth int) []string {
	t.Helper()

	if depth > 5 {
		t.Errorf("%s: cannot resolve %s", pos, exprText(e))
		return nil
	}

	switch x := e.(type) {
	case *ast.BasicLit:
		s, err := strconv.Unquote(x.Value)
		if err != nil || x.Kind != token.STRING {
			t.Errorf("%s: %s is not a string", pos, x.Value)
		}

		return []string{s}
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && exprText(sel) == "fmt.Sprintf" && len(x.Args) > 0 {
			var out []string

			for _, format := range p.values(t, pos, fn, x.Args[0], depth+1) {
				out = append(out, printfVerb.ReplaceAllStringFunc(format, func(v string) string {
					if v != "%d" {
						t.Errorf("%s: format verb %s is not handled", pos, v)
					}

					return "7"
				}))
			}

			return out
		}
	case *ast.Ident:
		if fn != nil {
			if values, ok := p.local(t, pos, fn, x.Name, depth); ok {
				return values
			}
		}

		if c, ok := p.consts[x.Name]; ok {
			return p.values(t, pos, nil, c, depth+1)
		}
	}

	t.Errorf("%s: cannot resolve %s", pos, exprText(e))

	return nil
}

// local returns the values of name inside fn, when it is a parameter or a
// variable of fn.
func (p *sourcePackage) local(t *testing.T, pos string, fn *ast.FuncDecl, name string, depth int) ([]string, bool) {
	t.Helper()

	param := -1
	i := 0

	for _, field := range fn.Type.Params.List {
		for _, n := range field.Names {
			if n.Name == name {
				param = i
			}

			i++
		}
	}

	var out []string

	found := false

	if param >= 0 {
		found = true

		for _, f := range p.files {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || calledName(call.Fun) != fn.Name.Name || param >= len(call.Args) {
					return true
				}

				out = append(out, p.values(t, pos, enclosing(f, call), call.Args[param], depth+1)...)

				return true
			})
		}

		return out, found
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			for j, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == name && j < len(s.Rhs) {
					out = append(out, p.values(t, pos, fn, s.Rhs[j], depth+1)...)
					found = true
				}
			}
		case *ast.ValueSpec:
			for j, id := range s.Names {
				if id.Name == name && j < len(s.Values) {
					out = append(out, p.values(t, pos, fn, s.Values[j], depth+1)...)
					found = true
				}
			}
		}

		return true
	})

	return out, found
}

// calledName is the name of the function or method a call calls.
func calledName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	default:
		return ""
	}
}

// enclosing is the function of f that holds n.
func enclosing(f *ast.File, n ast.Node) *ast.FuncDecl {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Pos() <= n.Pos() && n.End() <= fd.End() {
			return fd
		}
	}

	return nil
}

// exprText is a short text of e for the messages of the test.
func exprText(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprText(x.X) + "." + x.Sel.Name
	default:
		return fmt.Sprintf("%T", e)
	}
}
