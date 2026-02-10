package overlay

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
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

// Definition describes which package and functions to overlay.
type Definition struct {
	PkgPath   string
	Functions []*Function
	Template  string
}

// Function describes a function to be renamed.
type Function struct {
	Name   string
	Method *Method
}

// Method describes a method receiver.
type Method struct {
	Type    string
	Name    string
	Pointer bool
}

// PackageOverlay holds the result of rendering overlay for a single package.
type PackageOverlay struct {
	// Replace maps original .go file path → temp .go file path (for modified files).
	Replace map[string]string
	// Added contains paths of new files (tobari.go) to add to compile args.
	Added []string
	// Imports contains import paths from tobari.go (for importcfg resolution).
	Imports []string
}

//go:embed templates/*.tmpl
var tmpls embed.FS

const funcSuffix = "tobari"

var defs = []*Definition{
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
}

// TargetPackages returns a map of package import paths that need overlay,
// keyed by PkgPath with the corresponding Definition as value.
func TargetPackages() map[string]*Definition {
	m := make(map[string]*Definition, len(defs))
	for _, def := range defs {
		m[def.PkgPath] = def
	}
	return m
}

// ComputeHash computes a deterministic content hash of all overlay modifications.
// It performs all rendering in memory without writing any files.
func ComputeHash() (string, error) {
	type entry struct {
		origPath string
		content  []byte
	}
	var entries []entry

	for _, def := range defs {
		pkgPathStr, err := utils.GoPkgPath(def.PkgPath)
		if err != nil {
			return "", err
		}
		pkgFiles, err := utils.GoPkgFiles(pkgPathStr)
		if err != nil {
			return "", fmt.Errorf("failed to list package files for %s: %w", def.PkgPath, err)
		}

		pkgScopedReplacedNameMap := make(map[string]string)

		for _, pkgFile := range pkgFiles {
			src, err := os.ReadFile(pkgFile)
			if err != nil {
				return "", fmt.Errorf("failed to read package file %s: %w", pkgFile, err)
			}

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, pkgFile, src, 0)
			if err != nil {
				return "", fmt.Errorf("failed to parse package file %s: %w", pkgFile, err)
			}

			hasRename := false
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
				pkgScopedReplacedNameMap[fn.Name] = newName
				hasRename = true
			}
			if hasRename {
				var buf bytes.Buffer
				if err := format.Node(&buf, fset, file); err != nil {
					return "", fmt.Errorf("failed to format AST: %w", err)
				}
				entries = append(entries, entry{
					origPath: pkgFile,
					content:  buf.Bytes(),
				})
			}
		}

		b, err := evalTemplate(def.Template, pkgScopedReplacedNameMap)
		if err != nil {
			return "", err
		}
		entries = append(entries, entry{
			origPath: filepath.Join(pkgPathStr, "tobari.go"),
			content:  b,
		})
	}

	// Sort by origPath for deterministic hash
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].origPath < entries[j].origPath
	})

	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e.origPath))
		h.Write(e.content)
	}

	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// RenderPackage creates overlay files for a single package.
// sourceFiles are the .go files from compile args for this package.
// It parses each source file, renames target functions, renders the template,
// and writes all files to a content-hash-based directory to ensure deterministic
// paths across parent and child builds.
func RenderPackage(def *Definition, sourceFiles []string) (*PackageOverlay, error) {
	// First pass: render everything in memory
	type renderedFile struct {
		origPath string
		basename string
		content  []byte
	}
	var rendered []renderedFile
	pkgScopedReplacedNameMap := make(map[string]string)

	for _, srcFile := range sourceFiles {
		src, err := os.ReadFile(srcFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read source file %s: %w", srcFile, err)
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, srcFile, src, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to parse source file %s: %w", srcFile, err)
		}

		hasRename := false
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
			pkgScopedReplacedNameMap[fn.Name] = newName
			hasRename = true
		}
		if hasRename {
			var buf bytes.Buffer
			if err := format.Node(&buf, fset, file); err != nil {
				return nil, fmt.Errorf("failed to format AST: %w", err)
			}
			rendered = append(rendered, renderedFile{
				origPath: srcFile,
				basename: filepath.Base(srcFile),
				content:  buf.Bytes(),
			})
		}
	}

	// Render template
	tmplContent, err := evalTemplate(def.Template, pkgScopedReplacedNameMap)
	if err != nil {
		return nil, err
	}
	rendered = append(rendered, renderedFile{
		basename: "tobari.go",
		content:  tmplContent,
	})

	// Compute content hash for deterministic directory path
	h := sha256.New()
	for _, f := range rendered {
		h.Write(f.content)
	}
	id := hex.EncodeToString(h.Sum(nil))[:16]

	// Write to deterministic directory using atomic writes.
	// Since multiple builds may run concurrently, we write to a temp file
	// and rename to avoid partial reads.
	dir := filepath.Join(utils.OverlayDir(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create overlay dir: %w", err)
	}

	replace := make(map[string]string)
	for _, f := range rendered {
		path := filepath.Join(dir, f.basename)
		if err := atomicWriteFile(path, f.content); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", f.basename, err)
		}
		if f.origPath != "" {
			replace[f.origPath] = path
		}
	}

	tobariPath := filepath.Join(dir, "tobari.go")
	imports, err := utils.ImportsFromSource(tmplContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse imports from tobari.go: %w", err)
	}

	return &PackageOverlay{
		Replace: replace,
		Added:   []string{tobariPath},
		Imports: imports,
	}, nil
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

// atomicWriteFile writes content to path atomically using a temp file and rename.
// If the file already exists with the same content, it's a no-op.
func atomicWriteFile(path string, content []byte) error {
	// Check if file already exists with correct content
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return nil
	}

	// Write to temp file in the same directory (for same-filesystem rename)
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // Clean up on error

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// Atomic rename
	return os.Rename(tmpPath, path)
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
