package overlay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"text/template"

	"github.com/goccy/tobari/internal/utils"
)

func Create(ctx context.Context) (string, error) {
	overlay, err := createOverlay(ctx, []*Definition{
		{
			PkgPath: "runtime",
			Functions: []*Function{
				{Name: "coverage_getCovCounterList"},
			},
			Template: "runtime.go.tmpl",
		},
		{
			PkgPath: "testing",
			Functions: []*Function{
				{
					Name: "Run",
					Method: &Method{
						Type:    "T",
						Name:    "t",
						Pointer: true,
					},
				},
			},
			Template: "testing.go.tmpl",
		},
		{
			PkgPath:   "testing/internal/testdeps",
			Functions: []*Function{{Name: "coverTearDown"}},
			Template:  "testdeps.go.tmpl",
		},
	})
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(overlay)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(overlay.path, b, 0o600); err != nil {
		return "", err
	}
	return overlay.path, nil
}

type Overlay struct {
	// Replace contains original path -> overlay path mappings
	// Used by toolexec to replace files during compilation
	Replace map[string]string
	// ExportPaths contains package import paths -> compiled archive paths
	// for imports needed by tobari.go files
	ExportPaths map[string]string `json:",omitempty"`
	path        string
}

//go:embed templates/*.tmpl
var tmpls embed.FS

func createOverlay(ctx context.Context, defs []*Definition) (*Overlay, error) {
	root, err := OverlayRootDir()
	if err != nil {
		return nil, err
	}

	// Use fixed suffix "tobari" for function names to make hash deterministic
	const funcSuffix = "tobari"

	// First pass: collect all rendered content to compute hash
	type renderedFile struct {
		pkgPath  string
		fileName string
		content  []byte
		isNew    bool // true for template-generated files, false for modified files
		origPath string
	}
	var renderedFiles []renderedFile

	for _, def := range defs {
		pkgPathStr, err := utils.GoPkgPath(def.PkgPath)
		if err != nil {
			return nil, err
		}
		pkgFiles, err := utils.GoPkgFiles(pkgPathStr)
		if err != nil {
			return nil, fmt.Errorf("failed to list package files for %s: %w", def.PkgPath, err)
		}
		pkgScopedReplacedNameMap := make(map[string]string)

		for _, pkgFile := range pkgFiles {
			src, err := os.ReadFile(pkgFile)
			if err != nil {
				return nil, fmt.Errorf("failed to read package file %s: %w", pkgFile, err)
			}

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, pkgFile, src, 0)
			if err != nil {
				return nil, fmt.Errorf("failed to parse package file %s: %w", pkgFile, err)
			}

			fileScopedReplacedNameMap := make(map[string]string)
			for _, decl := range file.Decls {
				funcDecl, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				fn := matchedFunc(def, funcDecl)
				if fn == nil {
					continue
				}
				newName := fn.Name + "_" + funcSuffix
				funcDecl.Name = &ast.Ident{Name: newName}
				fileScopedReplacedNameMap[fn.Name] = newName
				pkgScopedReplacedNameMap[fn.Name] = newName
			}
			if len(fileScopedReplacedNameMap) != 0 {
				var buf bytes.Buffer
				if err := format.Node(&buf, fset, file); err != nil {
					return nil, fmt.Errorf("failed to format AST: %w", err)
				}
				renderedFiles = append(renderedFiles, renderedFile{
					pkgPath:  def.PkgPath,
					fileName: filepath.Base(pkgFile),
					content:  buf.Bytes(),
					isNew:    false,
					origPath: pkgFile,
				})
			}
		}

		b, err := evalTemplate(def.Template, pkgScopedReplacedNameMap)
		if err != nil {
			return nil, err
		}
		renderedFiles = append(renderedFiles, renderedFile{
			pkgPath:  def.PkgPath,
			fileName: "tobari.go",
			content:  b,
			isNew:    true,
			origPath: filepath.Join(pkgPathStr, "tobari.go"),
		})
	}

	// Compute hash from all rendered content
	h := sha256.New()
	// Sort by origPath to ensure deterministic ordering
	sort.Slice(renderedFiles, func(i, j int) bool {
		return renderedFiles[i].origPath < renderedFiles[j].origPath
	})
	for _, rf := range renderedFiles {
		h.Write([]byte(rf.origPath))
		h.Write(rf.content)
	}
	id := hex.EncodeToString(h.Sum(nil))[:16]

	// Create directories and write files
	if err := os.MkdirAll(filepath.Join(root, id), 0o755); err != nil {
		return nil, err
	}

	overlayMap := make(map[string]string)

	// Collect all imports from tobari.go files
	allImports := make(map[string]struct{})
	for _, rf := range renderedFiles {
		dirPath := filepath.Join(root, id, rf.pkgPath)
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			return nil, err
		}
		tmpFile := filepath.Join(dirPath, rf.fileName)
		if err := os.WriteFile(tmpFile, rf.content, 0o600); err != nil {
			return nil, err
		}
		overlayMap[rf.origPath] = tmpFile

		// For tobari.go files, collect their imports
		if rf.isNew {
			imports, err := utils.ImportsFromSource(rf.content)
			if err != nil {
				return nil, fmt.Errorf("failed to parse imports from %s: %w", rf.origPath, err)
			}
			for _, imp := range imports {
				allImports[imp] = struct{}{}
			}
		}
	}

	// Collect export paths for imports
	importList := make([]string, 0, len(allImports))
	for imp := range allImports {
		// Skip unsafe as it doesn't have an export file
		if imp == "unsafe" {
			continue
		}
		importList = append(importList, imp)
	}
	exportMap, err := utils.GoListExportMap(importList)
	if err != nil {
		return nil, fmt.Errorf("failed to collect export paths: %w", err)
	}

	path, err := OverlayPath()
	if err != nil {
		return nil, err
	}
	return &Overlay{Replace: overlayMap, ExportPaths: exportMap, path: path}, nil
}

type Definition struct {
	PkgPath   string
	Functions []*Function
	Template  string
}

type Function struct {
	Name   string
	Method *Method
}

type Method struct {
	Type    string
	Name    string
	Pointer bool
}

func evalTemplate(path string, replacedNameMap map[string]string) ([]byte, error) {
	f, err := tmpls.ReadFile(filepath.Join("templates", path))
	if err != nil {
		return nil, fmt.Errorf("failed to read template: %w", err)
	}
	tmpl, err := template.New(path).Parse(string(f))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template %s: %w", path, err)
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, replacedNameMap); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}
	return b.Bytes(), nil
}

func matchedFunc(def *Definition, decl *ast.FuncDecl) *Function {
	for _, fn := range def.Functions {
		if decl.Name.Name != fn.Name {
			continue
		}
		if fn.Method != nil {
			if decl.Recv == nil {
				continue
			}
			if len(decl.Recv.List) == 0 {
				continue
			}
			if fn.Method.Pointer {
				star, ok := decl.Recv.List[0].Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				ident, ok := star.X.(*ast.Ident)
				if !ok {
					continue
				}
				if ident.Name == fn.Method.Type {
					return fn
				}
			}
		} else {
			if decl.Recv != nil {
				continue
			}
			return fn
		}
	}
	return nil
}

// GetReplace reads overlay.json and returns the replacement paths.
func GetReplace() (map[string]string, error) {
	path, err := OverlayPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var overlay Overlay
	if err := json.Unmarshal(data, &overlay); err != nil {
		return nil, err
	}

	return overlay.Replace, nil
}

// GetExportPaths reads overlay.json and returns the export paths for imports
func GetExportPaths() (map[string]string, error) {
	path, err := OverlayPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var overlay Overlay
	if err := json.Unmarshal(data, &overlay); err != nil {
		return nil, err
	}

	return overlay.ExportPaths, nil
}
