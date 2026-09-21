package errs_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/errs"
)

// TestEveryCodeIsInTheCatalog keeps the published list of error codes honest.
//
// The catalog is documentation: the console, `docs/api.md` and anything else
// reading the reference get their list of codes from it. Go cannot enumerate
// constants at runtime, so the source is parsed instead — a new code with no
// entry fails here rather than quietly going undocumented, which is the whole
// mechanism. The alternative is remembering, and remembering is what leaves a
// reference describing last month's API.
func TestEveryCodeIsInTheCatalog(t *testing.T) {
	declared := codesDeclaredInSource(t)
	require.NotEmpty(t, declared, "the source parse found nothing, so this test proves nothing")

	documented := map[errs.Code]bool{}
	for _, entry := range errs.Catalog() {
		documented[entry.Code] = true
		require.NotEmpty(t, entry.Meaning, "%s has an empty meaning", entry.Code)
		require.NotZero(t, entry.Status, "%s has no HTTP status", entry.Code)
	}

	for _, code := range declared {
		require.True(t, documented[code],
			"%s is not in the catalog. Add it to `meanings` in catalog.go — a client branching on "+
				"a code it cannot look up is the reason that list exists.", code)
	}
	for code := range documented {
		require.Contains(t, declared, code,
			"%s is in the catalog but is no longer declared. Remove it.", code)
	}
}

// codesDeclaredInSource reads errs.go for `X Code = "..."` constants.
func codesDeclaredInSource(t *testing.T) []errs.Code {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "errs.go", nil, 0)
	require.NoError(t, err)

	var out []errs.Code
	var currentType string

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// A const block carries the type on the first spec and repeats it
			// implicitly, so the last one seen is the type in force.
			if ident, ok := value.Type.(*ast.Ident); ok {
				currentType = ident.Name
			}
			if currentType != "Code" {
				continue
			}
			for _, v := range value.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				out = append(out, errs.Code(lit.Value[1:len(lit.Value)-1]))
			}
		}
	}
	return out
}
