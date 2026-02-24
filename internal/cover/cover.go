// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cover implements coverage instrumentation functionality.
// Some code in this file is derived from Go's coverage tool.
package cover

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/tobari/internal/tobari"
)

func Run(ctx context.Context, args []string, embedCode bool) error {
	inputFiles, opt, err := parseOption(args)
	if err != nil {
		return err
	}
	if len(inputFiles) == 0 {
		return nil
	}

	f, err := os.ReadFile(inputFiles[0])
	if err != nil {
		return err
	}

	var pkgcfg *PackageConfig
	if opt.pkgcfg != "" {
		cfg, err := readPackageConfig(opt.pkgcfg)
		if err != nil {
			return err
		}
		pkgcfg = cfg
	} else {
		file, err := parser.ParseFile(new(token.FileSet), "", string(f), parser.PackageClauseOnly)
		if err != nil {
			return err
		}
		pkgcfg = &PackageConfig{
			PkgName: file.Name.String(),
		}
	}
	if pkgcfg.EmitMetaFile != "" {
		if err := os.WriteFile(pkgcfg.EmitMetaFile, nil, 0o600); err != nil {
			return fmt.Errorf("failed to write metadata file to %s: %w", pkgcfg.EmitMetaFile, err)
		}
	}

	// Only embed code when embedCode is enabled, not in testmain mode, and pkgcfg is available
	shouldEmbed := embedCode && opt.mode != "testmain" && opt.pkgcfg != ""

	var depMap *FunctionDependency

	// When using "go test", it eventually invokes go tool cover with the `-mode testmain` flag,
	// but the target files in this case are temporary files written under the $WORK directory.
	// As a result, there is no go.mod file present, and the files cannot be built correctly.
	// Consequently, a dependency map cannot be created, so the generation process is skipped altogether.
	if opt.mode != "testmain" {
		dep, err := createFunctionDependencyMap(pkgcfg, inputFiles[0])
		if err != nil {
			return err
		}
		depMap = dep
	} else {
		if err := addTobariImportToTestMain(inputFiles[0]); err != nil {
			return err
		}
	}
	if len(inputFiles) == 1 && opt.output != "" {
		if err := annotateFile(pkgcfg, depMap, inputFiles[0], opt.output, opt.mode); err != nil {
			return err
		}
		outputFiles := []string{opt.output}
		if opt.outputFileList != "" {
			if err := writeOutputFileList(opt.outputFileList, outputFiles); err != nil {
				return err
			}
		}
		if err := createCovervars(pkgcfg, opt.pkgcfg, inputFiles, shouldEmbed); err != nil {
			return err
		}
		return nil
	}

	outputFiles := make([]string, 0, len(inputFiles))
	for _, inputFile := range inputFiles {
		base := filepath.Base(inputFile)
		if filepath.Ext(base) == ".go" {
			base = base[:len(base)-len(filepath.Ext(base))]
		}
		outputName := base + ".cover.go"
		if err := annotateFile(pkgcfg, depMap, inputFile, outputName, opt.mode); err != nil {
			return err
		}
		outputFiles = append(outputFiles, outputName)
	}
	if err := writeOutputFileList(opt.outputFileList, outputFiles); err != nil {
		return err
	}
	if err := createCovervars(pkgcfg, opt.pkgcfg, inputFiles, shouldEmbed); err != nil {
		return err
	}
	return nil
}

// CoverPkgConfig is a bundle of information passed from the Go
// command to the cover command during "go build -cover" runs. The
// Go command creates and fills in a struct as below, then passes
// file containing the encoded JSON for the struct to the "cover"
// tool when instrumenting the source files in a Go package.
type PackageConfig struct {
	// File into which cmd/cover should emit summary info
	// when instrumentation is complete.
	OutConfig string

	// Import path for the package being instrumented.
	PkgPath string

	// Package name.
	PkgName string

	// Instrumentation granularity: one of "perfunc" or "perblock" (default)
	Granularity string

	// Module path for this package (empty if no go.mod in use)
	ModulePath string

	// Local mode indicates we're doing a coverage build or test of a
	// package selected via local import path, e.g. "./..." or
	// "./foo/bar" as opposed to a non-relative import path. See the
	// corresponding field in cmd/go's PackageInternal struct for more
	// info.
	Local bool

	// EmitMetaFile if non-empty is the path to which the cover tool should
	// directly emit a coverage meta-data file for the package, if the
	// package has any functions in it. The go command will pass in a value
	// here if we've been asked to run "go test -cover" on a package that
	// doesn't have any *_test.go files.
	EmitMetaFile string
}

func readPackageConfig(path string) (*PackageConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read pkgconfig file %q: %v", path, err)
	}
	var pkgcfg PackageConfig
	if err := json.Unmarshal(data, &pkgcfg); err != nil {
		return nil, fmt.Errorf("error reading pkgconfig file %q: %v", path, err)
	}
	return &pkgcfg, nil
}

func createCovervars(pkgcfg *PackageConfig, pkgcfgPath string, inputFiles []string, embedCode bool) error {
	if pkgcfgPath == "" {
		return nil
	}
	covervarsPath := strings.Replace(pkgcfgPath, "pkgcfg.txt", "covervars.go", 1)

	var constDecls strings.Builder
	var addSourceCalls strings.Builder
	for i, inputFile := range inputFiles {
		compressed := `""`
		if embedCode {
			c, err := compressFileContent(inputFile)
			if err != nil {
				return fmt.Errorf("failed to compress %s: %w", inputFile, err)
			}
			compressed = c
		}
		constName := fmt.Sprintf("%s_src_%d", tobariPkg, i)
		fmt.Fprintf(&constDecls, "const %s = %s\n", constName, compressed)
		fmt.Fprintf(&addSourceCalls, "\t%s_AddEmbeddedSource(%q, %s)\n", tobariPkg, inputFile, constName)
	}

	src := fmt.Sprintf(`
// Code generated by tobari.
package %[1]s

import (
   "unsafe"
)

%[3]s
//go:linkname %[2]s_GID runtime.GID
func %[2]s_GID() uint64

//go:linkname %[2]s_PGID runtime.PGID
func %[2]s_PGID() uint64

//go:linkname %[2]s_Trace github.com/goccy/tobari/internal/tobari.Trace
func %[2]s_Trace(string, uint64, uint64, int, int, int, int, int, int)

//go:linkname %[2]s_SetGIDFunc github.com/goccy/tobari/internal/tobari.SetGIDFunc
func %[2]s_SetGIDFunc(func() uint64) bool

//go:linkname %[2]s_AddCoverMeta github.com/goccy/tobari/internal/tobari.AddCoverMeta
func %[2]s_AddCoverMeta(string) bool

//go:linkname %[2]s_AddEmbeddedSource github.com/goccy/tobari/internal/tobari.AddEmbeddedSource
func %[2]s_AddEmbeddedSource(string, string) bool

//go:linkname %[2]s_TraceChan github.com/goccy/tobari/internal/tobari.TraceChan
func %[2]s_TraceChan(uintptr, uint64, bool)

func %[2]s_chanID(ch any) uintptr {
	return (*[2]uintptr)(unsafe.Pointer(&ch))[1]
}

func %[2]s_afterRecv[T any](chanID uintptr, v T) T {
	%[2]s_TraceChan(chanID, %[2]s_GID(), false)
	return v
}

func %[2]s_beforeSend[T any](chanID uintptr, v T) T {
	%[2]s_TraceChan(chanID, %[2]s_GID(), true)
	return v
}

var _ = unsafe.Pointer(nil)

var _ = %[2]s_SetGIDFunc(func() uint64 { return %[2]s_GID() })
var _ = func() bool {
%[4]s
	return true
}()
`, pkgcfg.PkgName, tobariPkg, constDecls.String(), addSourceCalls.String())
	return os.WriteFile(covervarsPath, []byte(src), 0o600)
}

// compressFileContent reads a file and returns its gzip-compressed content
// as a Go string literal (quoted with fmt.Sprintf %q).
func compressFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := gw.Write(data); err != nil {
		return "", err
	}
	if err := gw.Close(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%q", buf.String()), nil
}

type Option struct {
	mode           string
	output         string
	outputFileList string
	pkgcfg         string
}

func parseOption(args []string) ([]string, *Option, error) {
	fs := flag.NewFlagSet("cover", flag.ContinueOnError)
	mode := fs.String("mode", "count", "coverage mode: count, atomic")
	output := fs.String("o", "", "output file")
	outfilelist := fs.String("outfilelist", "", "file containing list of output files")
	pkgcfg := fs.String("pkgcfg", "", "package configuration file")
	_ = fs.String("var", "", "name of coverage variable prefix")
	_ = fs.String("V", "", "version flag")

	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	return fs.Args(), &Option{
		mode:           *mode,
		output:         *output,
		outputFileList: *outfilelist,
		pkgcfg:         *pkgcfg,
	}, nil
}

func writeOutputFileList(filename string, outputFiles []string) (e error) {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer func() {
		e = file.Close()
	}()

	for _, outFile := range outputFiles {
		if _, err := fmt.Fprintf(file, "%s\n", outFile); err != nil {
			return err
		}
	}
	return nil
}

func annotateFile(pkgcfg *PackageConfig, dep *FunctionDependency, src, dst, mode string) error {
	if dst != "" {
		return createFile(pkgcfg, dep, src, dst, mode)
	}
	b, err := addTracePoint(pkgcfg, dep, src, mode)
	if err != nil {
		return err
	}
	if _, err := os.Stdout.Write(b); err != nil {
		return err
	}
	return nil
}

func createFile(pkgcfg *PackageConfig, dep *FunctionDependency, src, dst, mode string) error {
	converted, err := addTracePoint(pkgcfg, dep, src, mode)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, converted, 0o600); err != nil {
		return err
	}
	return nil
}

func addTracePoint(pkgcfg *PackageConfig, dep *FunctionDependency, src, mode string) ([]byte, error) {
	f, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	return addTracePointWithContent(pkgcfg, dep, src, f, mode)
}

type File struct {
	fset                *token.FileSet
	name                string
	mode                string
	astFile             *ast.File
	curFunc             *Function
	funcs               []*Function
	content             []byte
	edit                *Buffer
	funcDep             *FunctionDependency
	pkgcfg              *PackageConfig
	anonymGlobalFuncIdx int
	commClauseRecvPos   map[token.Pos]struct{} // positions of <-ch inside CommClause.Comm to skip wrapping
}

func (f *File) nextBlockIndex() int {
	var ret int
	for _, fn := range f.funcs {
		ret += len(fn.blocks)
	}
	return ret
}

type Function struct {
	name          string
	blocks        []*tobari.Block
	anonymFuncIdx int
}

func newFunction(name string) *Function {
	return &Function{
		name:          name,
		anonymFuncIdx: 1,
	}
}

func (f *Function) addBlock(b *tobari.Block) {
	if f == nil {
		return
	}
	f.blocks = append(f.blocks, b)
}

func addTracePointWithContent(pkgcfg *PackageConfig, dep *FunctionDependency, filename string, content []byte, mode string) ([]byte, error) {
	fset := token.NewFileSet()
	parsedFile, err := parser.ParseFile(fset, filename, content, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	file := &File{
		fset:                fset,
		mode:                mode,
		name:                filename,
		content:             content,
		edit:                NewBuffer(content),
		astFile:             parsedFile,
		funcDep:             dep,
		pkgcfg:              pkgcfg,
		anonymGlobalFuncIdx: 1,
		commClauseRecvPos:   collectCommClauseRecvPos(parsedFile),
	}

	// Walk the AST and instrument code
	ast.Walk(file, file.astFile)

	footer, err := file.renderFooter()
	if err != nil {
		return nil, err
	}
	return append(file.edit.Bytes(), []byte(footer)...), nil
}

const (
	tobariPkg = "github_com_goccy_tobari"
)

func (f *File) renderFooter() (string, error) {
	md, err := f.renderMetadata()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`
var _ = %s_AddCoverMeta(%q)
`, tobariPkg, md), nil
}

func (f *File) renderMetadata() (string, error) {
	funcs := make([]*tobari.Function, 0, len(f.funcs))
	for _, fn := range f.funcs {
		// When "-mode testmain" is specified, funcDep becomes nil, so the process is skipped.
		if f.funcDep == nil {
			continue
		}
		deps, exists := f.funcDep.DepMap[fn.name]
		if !exists {
			depNames := make([]string, 0, len(f.funcDep.DepMap))
			for name := range f.funcDep.DepMap {
				depNames = append(depNames, name)
			}
			return "", fmt.Errorf("failed to find function dependencies %s from %v", fn.name, depNames)
		}
		funcs = append(funcs, &tobari.Function{
			Name:   fn.name,
			Blocks: fn.blocks,
			Deps:   deps,
		})
	}
	b, err := json.Marshal(&tobari.Metadata{
		FileName:   f.name,
		PkgPath:    f.pkgcfg.PkgPath,
		PkgName:    f.pkgcfg.PkgName,
		ModulePath: f.pkgcfg.ModulePath,
		Funcs:      funcs,
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode tobari's metadata: %w", err)
	}
	return string(b), nil
}

func (f *File) offset(pos token.Pos) int {
	return f.fset.Position(pos).Offset
}

func (f *File) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.BlockStmt:
		// Handle block statements like the original cover tool
		if len(n.List) > 0 {
			switch n.List[0].(type) {
			case *ast.CaseClause: // switch
				for _, stmt := range n.List {
					clause := stmt.(*ast.CaseClause)
					f.addCounters(clause.Colon+1, clause.Colon+1, clause.End(), clause.Body, false)
				}
				return f
			case *ast.CommClause: // select
				for _, stmt := range n.List {
					clause := stmt.(*ast.CommClause)
					if f.funcDep != nil {
						if chSrc, isSend := f.channelExprFromComm(clause.Comm); chSrc != "" {
							if isSend {
								// Send in select: wrap value expression via SendStmt case
								// (handled when the walk visits the SendStmt inside Comm)
							} else {
								// Receive in select: insert TraceChan at case body start
								f.edit.Insert(f.offset(clause.Colon)+1,
									fmt.Sprintf("%s_TraceChan(%s_chanID(%s),%s_GID(),false);", tobariPkg, tobariPkg, chSrc, tobariPkg))
							}
						}
					}
					f.addCounters(clause.Colon+1, clause.Colon+1, clause.End(), clause.Body, false)
				}
				return f
			}
		}
		f.addCounters(n.Lbrace, n.Lbrace+1, n.Rbrace+1, n.List, true)
	case *ast.IfStmt:
		if n.Init != nil {
			ast.Walk(f, n.Init)
		}
		ast.Walk(f, n.Cond)
		ast.Walk(f, n.Body)
		if n.Else == nil {
			return nil
		}
		// The elses are special, because if we have
		//	if x {
		//	} else if y {
		//	}
		// we want to cover the "if y". To do this, we need a place to drop the counter,
		// so we add a hidden block:
		//	if x {
		//	} else {
		//		if y {
		//		}
		//	}
		elseOffset := f.findText(n.Body.End(), "else")
		if elseOffset < 0 {
			panic("lost else")
		}
		f.edit.Insert(elseOffset+4, "{")
		f.edit.Insert(f.offset(n.Else.End()), "}")

		// We just created a block, now walk it.
		// Adjust the position of the new block to start after
		// the "else". That will cause it to follow the "{"
		// we inserted above.
		pos := f.fset.File(n.Body.End()).Pos(elseOffset + 4)
		switch stmt := n.Else.(type) {
		case *ast.IfStmt:
			block := &ast.BlockStmt{
				Lbrace: pos,
				List:   []ast.Stmt{stmt},
				Rbrace: stmt.End(),
			}
			n.Else = block
		case *ast.BlockStmt:
			stmt.Lbrace = pos
		default:
			panic("unexpected node type in if")
		}
		ast.Walk(f, n.Else)
		return nil
	case *ast.SelectStmt:
		// Don't annotate an empty select - creates a syntax error.
		if n.Body == nil || len(n.Body.List) == 0 {
			return nil
		}
	case *ast.SwitchStmt:
		// Don't annotate an empty switch - creates a syntax error.
		if n.Body == nil || len(n.Body.List) == 0 {
			if n.Init != nil {
				ast.Walk(f, n.Init)
			}
			if n.Tag != nil {
				ast.Walk(f, n.Tag)
			}
			return nil
		}
	case *ast.TypeSwitchStmt:
		// Don't annotate an empty type switch - creates a syntax error.
		if n.Body == nil || len(n.Body.List) == 0 {
			if n.Init != nil {
				ast.Walk(f, n.Init)
			}
			ast.Walk(f, n.Assign)
			return nil
		}
	case *ast.FuncDecl:
		// Don't instrument functions with blank names or bodyless functions
		if n.Name.Name == "_" || n.Body == nil {
			return nil
		}
		fname := n.Name.Name
		if f.funcDep != nil && len(f.funcDep.FuncNames) != 0 {
			// SSA's Function.Pos() returns FuncDecl.Name position (not the func keyword).
			pos := f.fset.Position(n.Name.Pos())
			key := funcPos{Filename: filepath.Clean(pos.Filename), Offset: pos.Offset}
			if fqdn, ok := f.funcDep.FuncNames[key]; ok {
				fname = fqdn
			}
		}
		parent := f.curFunc
		fn := newFunction(fname)
		f.funcs = append(f.funcs, fn)
		f.curFunc = fn
		ast.Walk(f, n.Body)
		f.curFunc = parent
		return nil
	case *ast.FuncLit:
		parent := f.curFunc
		var fname string
		if f.funcDep != nil && len(f.funcDep.FuncNames) != 0 {
			pos := f.fset.Position(n.Pos())
			key := funcPos{Filename: filepath.Clean(pos.Filename), Offset: pos.Offset}
			fname = f.funcDep.FuncNames[key]
		}
		if fname == "" {
			fname = f.createAnonymFuncName(parent)
		}
		fn := newFunction(fname)
		f.curFunc = fn
		f.funcs = append(f.funcs, fn)
		ast.Walk(f, n.Body)
		f.curFunc = parent
		return nil
	case *ast.SendStmt:
		// ch <- value → ch <- _tobari_beforeSend(chanID, value)
		if f.funcDep != nil {
			chSrc := string(f.content[f.offset(n.Chan.Pos()):f.offset(n.Chan.End())])
			f.edit.Insert(f.offset(n.Value.Pos()),
				fmt.Sprintf("%s_beforeSend(%s_chanID(%s),", tobariPkg, tobariPkg, chSrc))
			f.edit.Insert(f.offset(n.Value.End()), ")")
		}
	case *ast.UnaryExpr:
		// <-ch → _tobari_afterRecv(chanID, <-ch)
		if n.Op == token.ARROW && f.funcDep != nil {
			if _, skip := f.commClauseRecvPos[n.Pos()]; !skip {
				chSrc := string(f.content[f.offset(n.X.Pos()):f.offset(n.X.End())])
				f.edit.Insert(f.offset(n.Pos()),
					fmt.Sprintf("%s_afterRecv(%s_chanID(%s),", tobariPkg, tobariPkg, chSrc))
				f.edit.Insert(f.offset(n.End()), ")")
			}
		}
	case *ast.RangeStmt:
		// for v := range ch { → insert TraceChan at loop body start
		if f.funcDep != nil && f.funcDep.ChanRanges != nil {
			pos := f.fset.Position(n.X.Pos())
			key := funcPos{Filename: filepath.Clean(pos.Filename), Offset: pos.Offset}
			if _, ok := f.funcDep.ChanRanges[key]; ok {
				chSrc := string(f.content[f.offset(n.X.Pos()):f.offset(n.X.End())])
				f.edit.Insert(f.offset(n.Body.Lbrace)+1,
					fmt.Sprintf("%s_TraceChan(%s_chanID(%s),%s_GID(),false);", tobariPkg, tobariPkg, chSrc, tobariPkg))
			}
		}
	}
	return f
}

func (f *File) createAnonymFuncName(fn *Function) string {
	if fn == nil {
		defer func() { f.anonymGlobalFuncIdx++ }()
		return fmt.Sprintf("init$%d", f.anonymGlobalFuncIdx)
	}
	defer func() { fn.anonymFuncIdx++ }()
	return fmt.Sprintf("%s$%d", fn.name, fn.anonymFuncIdx)
}

// collectCommClauseRecvPos pre-scans the AST to find all receive expressions
// inside CommClause.Comm (select case statements). These positions are used to
// skip wrapping with afterRecv, since select case syntax does not allow it.
func collectCommClauseRecvPos(file *ast.File) map[token.Pos]struct{} {
	pos := make(map[token.Pos]struct{})
	ast.Inspect(file, func(n ast.Node) bool {
		cc, ok := n.(*ast.CommClause)
		if !ok {
			return true
		}
		if cc.Comm == nil {
			return true
		}
		// Collect all receive UnaryExpr positions inside Comm
		ast.Inspect(cc.Comm, func(inner ast.Node) bool {
			if ue, ok := inner.(*ast.UnaryExpr); ok && ue.Op == token.ARROW {
				pos[ue.Pos()] = struct{}{}
			}
			return true
		})
		return true
	})
	return pos
}

// channelExprFromComm extracts the channel source expression and whether it's a
// send operation from a CommClause's Comm statement.
func (f *File) channelExprFromComm(comm ast.Stmt) (string, bool) {
	if comm == nil {
		return "", false
	}
	switch s := comm.(type) {
	case *ast.SendStmt:
		return string(f.content[f.offset(s.Chan.Pos()):f.offset(s.Chan.End())]), true
	case *ast.ExprStmt:
		if unary, ok := s.X.(*ast.UnaryExpr); ok && unary.Op == token.ARROW {
			return string(f.content[f.offset(unary.X.Pos()):f.offset(unary.X.End())]), false
		}
	case *ast.AssignStmt:
		for _, rhs := range s.Rhs {
			if unary, ok := rhs.(*ast.UnaryExpr); ok && unary.Op == token.ARROW {
				return string(f.content[f.offset(unary.X.Pos()):f.offset(unary.X.End())]), false
			}
		}
	}
	return "", false
}

// findText finds text in the original source, starting at pos.
// It correctly skips over comments and assumes it need not
// handle quoted strings.
// It returns a byte offset within f.src.
func (f *File) findText(pos token.Pos, text string) int {
	b := []byte(text)
	start := f.offset(pos)
	i := start
	s := f.content
	for i < len(s) {
		if bytes.HasPrefix(s[i:], b) {
			return i
		}
		if i+2 <= len(s) && s[i] == '/' && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if i+2 <= len(s) && s[i] == '/' && s[i+1] == '*' {
			for i += 2; ; i++ {
				if i+2 > len(s) {
					return 0
				}
				if s[i] == '*' && s[i+1] == '/' {
					i += 2
					break
				}
			}
			continue
		}
		i++
	}
	return -1
}

// addCounters takes a list of statements and adds counters to the beginning of
// each basic block at the top level of that list. For instance, given
//
//	S1
//	if cond {
//		S2
//	}
//	S3
//
// counters will be added before S1 and before S3. The block containing S2
// will be visited in a separate call.
// TODO: Nested simple blocks get unnecessary (but correct) counters
func (f *File) addCounters(pos, insertPos, blockEnd token.Pos, list []ast.Stmt, extendToClosingBrace bool) {
	// Special case: make sure we add a counter to an empty block. Can't do this below
	// or we will add a counter to an empty statement list after, say, a return statement.
	if len(list) == 0 {
		f.edit.Insert(f.offset(insertPos), f.newCounter(insertPos, blockEnd, 0)+";")
		return
	}
	// Make a copy of the list, as we may mutate it and should leave the
	// existing list intact.
	list = append([]ast.Stmt(nil), list...)
	// We have a block (statement list), but it may have several basic blocks due to the
	// appearance of statements that affect the flow of control.
	for {
		// Find first statement that affects flow of control (break, continue, if, etc.).
		// It will be the last statement of this basic block.
		var last int
		end := blockEnd
		for last = 0; last < len(list); last++ {
			stmt := list[last]
			end = f.statementBoundary(stmt)
			if f.endsBasicSourceBlock(stmt) {
				if label, isLabel := stmt.(*ast.LabeledStmt); isLabel && !f.isControl(label.Stmt) {
					newLabel := *label
					newLabel.Stmt = &ast.EmptyStmt{
						Semicolon: label.Stmt.Pos(),
						Implicit:  true,
					}
					end = label.Pos() // Previous block ends before the label.
					list[last] = &newLabel
					// Open a gap and drop in the old statement, now without a label.
					list = append(list, nil)
					copy(list[last+1:], list[last:])
					list[last+1] = label.Stmt
				}
				last++
				extendToClosingBrace = false // Block is broken up now.
				break
			}
		}
		if extendToClosingBrace {
			end = blockEnd
		}
		if pos != end { // Can have no source to cover if e.g. blocks abut.
			f.edit.Insert(f.offset(insertPos), f.newCounter(pos, end, last)+";")
		}
		list = list[last:]
		if len(list) == 0 {
			break
		}
		pos = list[0].Pos()
		insertPos = pos
	}
}

// newCounter creates a new counter expression of the appropriate form.
func (f *File) newCounter(start, end token.Pos, numStmt int) string {
	stpos := f.fset.Position(start)
	enpos := f.fset.Position(end)

	// blockIndex (using the current block count as index)
	blockIndex := f.nextBlockIndex()

	// Generate both standard coverage call and our custom call
	// This ensures compatibility with Go's coverage runtime while adding our functionality.
	stmt := fmt.Sprintf(
		"%s_Trace(%q, %s_PGID(), %s_GID(), %d, %d, %d, %d, %d, %d)",
		tobariPkg,
		f.name,
		tobariPkg,
		tobariPkg,
		blockIndex,
		stpos.Line, enpos.Line,
		stpos.Column, enpos.Column,
		numStmt,
	)
	f.curFunc.addBlock(&tobari.Block{
		Idx: blockIndex,
		Start: tobari.Pos{
			Line: stpos.Line,
			Col:  stpos.Column,
		},
		End: tobari.Pos{
			Line: enpos.Line,
			Col:  enpos.Column,
		},
		NumStmts: numStmt,
	})
	return stmt
}

// statementBoundary finds the location in s that terminates the current basic
// block in the source.
func (f *File) statementBoundary(s ast.Stmt) token.Pos {
	// Control flow statements are easy.
	switch s := s.(type) {
	case *ast.BlockStmt:
		// Treat blocks like basic blocks to avoid overlapping counters.
		return s.Lbrace
	case *ast.IfStmt:
		if found, pos := hasFuncLiteral(s.Init); found {
			return pos
		}
		if found, pos := hasFuncLiteral(s.Cond); found {
			return pos
		}
		return s.Body.Lbrace
	case *ast.ForStmt:
		if found, pos := hasFuncLiteral(s.Init); found {
			return pos
		}
		if found, pos := hasFuncLiteral(s.Cond); found {
			return pos
		}
		if found, pos := hasFuncLiteral(s.Post); found {
			return pos
		}
		return s.Body.Lbrace
	case *ast.LabeledStmt:
		return f.statementBoundary(s.Stmt)
	case *ast.RangeStmt:
		if found, pos := hasFuncLiteral(s.X); found {
			return pos
		}
		return s.Body.Lbrace
	case *ast.SwitchStmt:
		if found, pos := hasFuncLiteral(s.Init); found {
			return pos
		}
		if found, pos := hasFuncLiteral(s.Tag); found {
			return pos
		}
		return s.Body.Lbrace
	case *ast.SelectStmt:
		return s.Body.Lbrace
	case *ast.TypeSwitchStmt:
		if found, pos := hasFuncLiteral(s.Init); found {
			return pos
		}
		return s.Body.Lbrace
	}

	if found, pos := hasFuncLiteral(s); found {
		return pos
	}
	return s.End()
}

// endsBasicSourceBlock reports whether s changes the flow of control: break, if, etc.,
// or if it's just problematic, for instance contains a function literal, which will complicate
// accounting due to the block-within-an expression.
func (f *File) endsBasicSourceBlock(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.BlockStmt:
		// Treat blocks like basic blocks to avoid overlapping counters.
		return true
	case *ast.BranchStmt:
		return true
	case *ast.ForStmt:
		return true
	case *ast.IfStmt:
		return true
	case *ast.LabeledStmt:
		return true // A goto may branch here, starting a new basic block.
	case *ast.RangeStmt:
		return true
	case *ast.SwitchStmt:
		return true
	case *ast.SelectStmt:
		return true
	case *ast.TypeSwitchStmt:
		return true
	case *ast.ExprStmt:
		// Calls to panic change the flow.
		// We really should verify that "panic" is the predefined function,
		// but without type checking we can't and the likelihood of it being
		// an actual problem is vanishingly small.
		if call, ok := s.X.(*ast.CallExpr); ok {
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "panic" && len(call.Args) == 1 {
				return true
			}
		}
	}
	found, _ := hasFuncLiteral(s)
	return found
}

// isControl reports whether s is a control statement that, if labeled, cannot be
// separated from its label.
func (f *File) isControl(s ast.Stmt) bool {
	switch s.(type) {
	case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.SelectStmt, *ast.TypeSwitchStmt:
		return true
	}
	return false
}

// hasFuncLiteral reports the existence and position of the first func literal
// in the node, if any. If a func literal appears, it usually marks the termination
// of a basic block because the function body is itself a block.
// Therefore we draw a line at the start of the body of the first function literal we find.
// TODO: what if there's more than one? Probably doesn't matter much.
func hasFuncLiteral(n ast.Node) (bool, token.Pos) {
	if n == nil {
		return false, 0
	}
	var literal funcLitFinder
	ast.Walk(&literal, n)
	return literal.found(), token.Pos(literal)
}

// funcLitFinder implements the ast.Visitor pattern to find the location of any
// function literal in a subtree.
type funcLitFinder token.Pos

func (f *funcLitFinder) Visit(node ast.Node) ast.Visitor {
	if f.found() {
		return nil // Prune search.
	}
	switch n := node.(type) {
	case *ast.FuncLit:
		*f = funcLitFinder(n.Body.Lbrace)
		return nil // Prune search.
	}
	return f
}

func (f *funcLitFinder) found() bool {
	return token.Pos(*f) != token.NoPos
}

// Buffer represents an edit buffer for making changes to source code
type Buffer struct {
	original []byte
	edits    []edit
}

type edit struct {
	start   int
	end     int
	newText string
	seq     int
}

// NewBuffer creates a new edit buffer
func NewBuffer(data []byte) *Buffer {
	return &Buffer{
		original: data,
		edits:    nil,
	}
}

// Insert adds text at the specified offset
func (b *Buffer) Insert(offset int, text string) {
	b.edits = append(b.edits, edit{
		start:   offset,
		end:     offset,
		newText: text,
		seq:     len(b.edits),
	})
}

// Replace replaces text from start to end with newText
func (b *Buffer) Replace(start, end int, newText string) {
	b.edits = append(b.edits, edit{
		start:   start,
		end:     end,
		newText: newText,
		seq:     len(b.edits),
	})
}

// Bytes returns the modified content
func (b *Buffer) Bytes() []byte {
	if len(b.edits) == 0 {
		return b.original
	}

	// Sort edits by start position (reverse order for correct application).
	// For same-position inserts, later-added edits (higher seq) come first
	// so they are applied first and end up closer to the original code.
	sort.SliceStable(b.edits, func(i, j int) bool {
		if b.edits[i].start != b.edits[j].start {
			return b.edits[i].start > b.edits[j].start
		}
		return b.edits[i].seq > b.edits[j].seq
	})

	result := make([]byte, len(b.original))
	copy(result, b.original)

	// Apply edits from end to beginning to avoid offset shifts
	for _, e := range b.edits {
		// Insert or replace
		before := result[:e.start]
		after := result[e.end:]

		newResult := make([]byte, 0, len(before)+len(e.newText)+len(after))
		newResult = append(newResult, before...)
		newResult = append(newResult, []byte(e.newText)...)
		newResult = append(newResult, after...)

		result = newResult
	}

	return result
}
