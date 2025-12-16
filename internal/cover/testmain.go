package cover

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
)

func addTobariImportToTestMain(inputPath string) error {
	content, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, inputPath, content, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse Go file: %w", err)
	}

	addTobariImportStmt(node)

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		return fmt.Errorf("failed to format: %w", err)
	}
	if err := os.WriteFile(inputPath, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}
	return nil
}

func addTobariImportStmt(file *ast.File) {
	if len(file.Decls) != 0 {
		if genDecl, ok := file.Decls[0].(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
			genDecl.Specs = append(genDecl.Specs, &ast.ImportSpec{
				Name: &ast.Ident{Name: "_"},
				Path: &ast.BasicLit{Kind: token.STRING, Value: `"github.com/goccy/tobari"`},
			})
			return
		}
	}
	file.Decls = append([]ast.Decl{
		&ast.GenDecl{
			Tok: token.IMPORT,
			Specs: []ast.Spec{
				&ast.ImportSpec{
					Name: &ast.Ident{Name: "_"},
					Path: &ast.BasicLit{Kind: token.STRING, Value: `"github.com/goccy/tobari"`},
				},
			},
		},
	}, file.Decls...)
}
