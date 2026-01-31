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
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
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
	// Replace contains paths that can be used with -overlay flag
	// (paths outside GOMODCACHE)
	Replace map[string]string
	// ToolexecReplace contains paths that must be replaced via toolexec
	// (paths inside GOMODCACHE, where -overlay is not allowed)
	ToolexecReplace map[string]string `json:",omitempty"`
	// ExportPaths contains package import paths -> compiled archive paths
	// for imports needed by tobari.go files
	ExportPaths map[string]string `json:",omitempty"`
	path        string
}

//go:embed templates/*.tmpl
var tmpls embed.FS

func createOverlay(ctx context.Context, defs []*Definition) (*Overlay, error) {
	root, err := OverlayRootDir(ctx)
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
		pkgPathStr, err := pkgPath(ctx, def.PkgPath)
		if err != nil {
			return nil, err
		}
		pkgFiles, err := pkgGoFiles(ctx, pkgPathStr)
		if err != nil {
			return nil, err
		}
		pkgScopedReplacedNameMap := make(map[string]string)

		for _, pkgFile := range pkgFiles {
			src, err := os.ReadFile(pkgFile)
			if err != nil {
				continue
			}

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, pkgFile, src, 0)
			if err != nil {
				continue
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

	// Get GOMODCACHE to determine which paths can use -overlay
	goModCache, _ := goModCachePath(ctx)

	overlayMap := make(map[string]string)
	toolexecReplaceMap := make(map[string]string)

	// Collect all imports from tobari.go files
	allImports := make(map[string]bool)
	for _, rf := range renderedFiles {
		dirPath := filepath.Join(root, id, rf.pkgPath)
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			return nil, err
		}
		tmpFile := filepath.Join(dirPath, rf.fileName)
		if err := os.WriteFile(tmpFile, rf.content, 0o600); err != nil {
			return nil, err
		}
		// Paths inside GOMODCACHE cannot be replaced via -overlay
		// They must be replaced via toolexec instead
		if goModCache != "" && strings.HasPrefix(rf.origPath, goModCache) {
			toolexecReplaceMap[rf.origPath] = tmpFile
		} else {
			overlayMap[rf.origPath] = tmpFile
		}

		// For tobari.go files, collect their imports
		if rf.isNew {
			imports, err := parseImportsFromFile(rf.content)
			if err == nil {
				for _, imp := range imports {
					allImports[imp] = true
				}
			}
		}
	}

	// Collect export paths for imports
	var importList []string
	for imp := range allImports {
		// Skip unsafe as it doesn't have an export file
		if imp == "unsafe" {
			continue
		}
		importList = append(importList, imp)
	}
	exportPaths, _ := collectExportPaths(ctx, importList)

	path, _ := OverlayPath(ctx)
	return &Overlay{Replace: overlayMap, ToolexecReplace: toolexecReplaceMap, ExportPaths: exportPaths, path: path}, nil
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

func pkgPath(ctx context.Context, pkg string) (string, error) {
	root, err := goRoot(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "src", pkg), nil
}

func pkgGoFiles(ctx context.Context, srcPath string) ([]string, error) {
	var ret []string
	_ = filepath.Walk(srcPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(info.Name()) != ".go" {
			return nil
		}

		if strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}

		ret = append(ret, path)
		return nil
	})
	return ret, nil
}

func goRoot(ctx context.Context) (string, error) {
	cmd, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("failed to find go binary path: %w", err)
	}
	out, err := exec.CommandContext(ctx, cmd, "env", "GOROOT").CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("failed to get GOROOT: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func goVersion(ctx context.Context) (string, error) {
	cmd, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("failed to find go binary path: %w", err)
	}
	out, err := exec.CommandContext(ctx, cmd, "env", "GOVERSION").CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("failed to get GOROOT: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func goModCachePath(ctx context.Context) (string, error) {
	cmd, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("failed to find go binary path: %w", err)
	}
	out, err := exec.CommandContext(ctx, cmd, "env", "GOMODCACHE").CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("failed to get GOMODCACHE: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// collectExportPaths runs `go list -export -json` for the given packages
// and returns a map of import path -> export file path
func collectExportPaths(ctx context.Context, packages []string) (map[string]string, error) {
	if len(packages) == 0 {
		return nil, nil
	}

	cmd, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("failed to find go binary path: %w", err)
	}

	args := append([]string{"list", "-export", "-json"}, packages...)
	out, err := exec.CommandContext(ctx, cmd, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run go list: %w", err)
	}

	result := make(map[string]string)
	decoder := json.NewDecoder(bytes.NewReader(out))
	for decoder.More() {
		var pkg struct {
			ImportPath string
			Export     string
		}
		if err := decoder.Decode(&pkg); err != nil {
			continue
		}
		if pkg.Export != "" {
			result[pkg.ImportPath] = pkg.Export
		}
	}
	return result, nil
}

// parseImportsFromFile parses a Go file and returns its imports
func parseImportsFromFile(content []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", content, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	var imports []string
	for _, imp := range file.Imports {
		// Remove quotes from import path
		path := strings.Trim(imp.Path.Value, "\"")
		imports = append(imports, path)
	}
	return imports, nil
}

// GetReplace reads overlay.json and returns all replacement paths
// (both Replace and ToolexecReplace merged together).
func GetReplace(ctx context.Context) (map[string]string, error) {
	path, err := OverlayPath(ctx)
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

	// Merge Replace and ToolexecReplace
	result := make(map[string]string, len(overlay.Replace)+len(overlay.ToolexecReplace))
	for k, v := range overlay.Replace {
		result[k] = v
	}
	for k, v := range overlay.ToolexecReplace {
		result[k] = v
	}

	return result, nil
}

// GetExportPaths reads overlay.json and returns the export paths for imports
func GetExportPaths(ctx context.Context) (map[string]string, error) {
	path, err := OverlayPath(ctx)
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
