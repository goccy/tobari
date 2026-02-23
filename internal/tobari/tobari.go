package tobari

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

func EnableCoverageCounting() {
	isEnabledTraceMu.Lock()
	isEnabledTrace = true
	isEnabledTraceMu.Unlock()
}

func DisableCoverageCounting() {
	isEnabledTraceMu.Lock()
	isEnabledTrace = false
	isEnabledTraceMu.Unlock()
}

func ClearCounters() {
	entryMapMu.Lock()
	gMapMu.Lock()

	entryMap = make(map[string]*TraceEntry)
	gMap = make(map[uint64]*TraceG)

	gMapMu.Unlock()
	entryMapMu.Unlock()
}

func CoverEntriesByName(name string) []*CoverEntry {
	entryMapMu.RLock()
	defer entryMapMu.RUnlock()

	for _, e := range entryMap {
		if e.Name != name {
			continue
		}
		return toEntries(e.CoverprofileMap())
	}
	return nil
}

func CoverprofileMap(mode string) map[string]string {
	entriesMap := CoverEntriesMap()
	ret := make(map[string]string, len(entriesMap))
	for k, entries := range entriesMap {
		b := bytes.NewBuffer([]byte(fmt.Sprintf("mode: %s\n", mode)))
		for _, entry := range entries {
			_, _ = fmt.Fprint(b, entry.String()+"\n")
		}
		ret[k] = b.String()
	}
	return ret
}

func CoverEntriesMap() map[string][]*CoverEntry {
	entryMapMu.RLock()
	defer entryMapMu.RUnlock()

	ret := make(map[string][]*CoverEntry)
	for _, e := range entryMap {
		ret[e.Name] = toEntries(e.CoverprofileMap())
	}
	return ret
}

func WriteCoverprofile(mode string, w io.Writer) {
	entryMapMu.RLock()
	defer entryMapMu.RUnlock()

	mergeMap := make(map[string]*CoverEntry)
	for _, e := range entryMap {
		for k, v := range e.CoverprofileMap() {
			mergeMap[k] = v
		}
	}
	_, _ = fmt.Fprint(w, renderMap(mode, mergeMap))
}

func WriteCoverprofileByName(name, mode string, w io.Writer) {
	entryMapMu.RLock()
	defer entryMapMu.RUnlock()

	for _, e := range entryMap {
		if e.Name != name {
			continue
		}
		_, _ = fmt.Fprint(w, renderMap(mode, e.CoverprofileMap()))
		return
	}
}

func WriteAllCoverprofile(mode string, w io.Writer) {
	gMapMu.RLock()
	defer gMapMu.RUnlock()

	blockToCountMap := make(map[string]int)
	for _, g := range gMap {
		g.blockToCountMap(blockToCountMap)
	}

	newCoverprofileMap := make(map[string]*CoverEntry)
	allCoverprofileMapMu.RLock()
	maps.Copy(newCoverprofileMap, allCoverprofileMap)
	allCoverprofileMapMu.RUnlock()

	for bid, count := range blockToCountMap {
		block := getBlock(bid)
		if block == nil {
			continue
		}
		newCoverprofileMap[bid] = &CoverEntry{
			FileName:  block.FileName,
			StartLine: block.Start.Line,
			StartCol:  block.Start.Col,
			EndLine:   block.End.Line,
			EndCol:    block.End.Col,
			NumStmts:  block.NumStmts,
			Count:     count,
		}
	}

	_, _ = fmt.Fprint(w, renderMap(mode, newCoverprofileMap))
}

func Cover(name, entryID string) {
	gid := currentGID()
	e := getEntry(entryID)
	if e == nil {
		e = &TraceEntry{Name: name}
		setEntry(entryID, e)
	}
	root := newTraceG(gid)
	e.Roots = append(e.Roots, root)
	setG(gid, root)
}

type Pos struct {
	Line int
	Col  int
}

type TraceEntry struct {
	Name  string
	Roots []*TraceG
}

type CoverEntry struct {
	FileName  string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	NumStmts  int
	Count     int
}

func (e *CoverEntry) String() string {
	return fmt.Sprintf(
		"%s:%d.%d,%d.%d %d %d",
		e.FileName,
		e.StartLine,
		e.StartCol,
		e.EndLine,
		e.EndCol,
		e.NumStmts,
		e.Count,
	)
}

func (e *TraceEntry) CoverprofileMap() map[string]*CoverEntry {
	blockToCountMap := make(map[string]int)
	for _, root := range e.Roots {
		root.blockToCountMap(blockToCountMap)
	}

	newCoverprofileMap := make(map[string]*CoverEntry)
	hitCandidateFuncMap := make(map[*Function]struct{})
	for bid, count := range blockToCountMap {
		block := getBlock(bid)
		if block == nil {
			continue
		}
		resolveCandidateFuncMap(block.Function, hitCandidateFuncMap)
		newCoverprofileMap[bid] = &CoverEntry{
			FileName:  block.FileName,
			StartLine: block.Start.Line,
			StartCol:  block.Start.Col,
			EndLine:   block.End.Line,
			EndCol:    block.End.Col,
			NumStmts:  block.NumStmts,
			Count:     count,
		}
	}
	for fn := range hitCandidateFuncMap {
		for _, block := range fn.Blocks {
			bid := blockID(block.FileName, block.Idx)
			if _, exists := newCoverprofileMap[bid]; exists {
				continue
			}
			newCoverprofileMap[bid] = &CoverEntry{
				FileName:  block.FileName,
				StartLine: block.Start.Line,
				StartCol:  block.Start.Col,
				EndLine:   block.End.Line,
				EndCol:    block.End.Col,
				NumStmts:  block.NumStmts,
			}
		}
	}
	return newCoverprofileMap
}

func resolveCandidateFuncMap(fn *Function, fnMap map[*Function]struct{}) {
	if _, exists := fnMap[fn]; exists {
		return
	}

	fnMap[fn] = struct{}{}
	funcMapMu.RLock()
	for _, dep := range fn.Deps {
		ref, exists := funcMap[dep]
		if !exists {
			panic(fmt.Sprintf("tobari: failed to find function reference by %s from %v\n", dep, funcNames))
		}
		fn.DepRefs = append(fn.DepRefs, ref)
	}
	funcMapMu.RUnlock()
	for _, ref := range fn.DepRefs {
		resolveCandidateFuncMap(ref, fnMap)
	}
}

func getEntry(id string) *TraceEntry {
	entryMapMu.RLock()
	defer entryMapMu.RUnlock()
	return entryMap[id]
}

func setEntry(id string, e *TraceEntry) {
	entryMapMu.Lock()
	entryMap[id] = e
	entryMapMu.Unlock()
}

type TraceG struct {
	ID              uint64
	BlockCounterMap map[string]int
	Children        []*TraceG
	mu              sync.RWMutex
}

func newTraceG(gid uint64) *TraceG {
	return &TraceG{
		ID:              gid,
		BlockCounterMap: make(map[string]int),
	}
}

func (g *TraceG) addCounter(blockID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.BlockCounterMap[blockID]++
}

func (g *TraceG) linkG(child *TraceG) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.Children = append(g.Children, child)
}

func (g *TraceG) blockToCountMap(blockToCountMap map[string]int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for blockID, count := range g.BlockCounterMap {
		blockToCountMap[blockID] += count
	}
	for _, child := range g.Children {
		child.blockToCountMap(blockToCountMap)
	}
}

var (
	isEnabledTraceMu       sync.RWMutex
	isEnabledTrace         = true
	gidFnOnce              sync.Once
	gidFn                  func() uint64
	entryMap               map[string]*TraceEntry
	entryMapMu             sync.RWMutex
	gMap                   map[uint64]*TraceG
	gMapMu                 sync.RWMutex
	blockMap               map[string]*Block
	blockMapMu             sync.RWMutex
	mdMu                   sync.RWMutex
	mds                    []*Metadata
	funcMap                map[string]*Function
	funcNames              []string
	funcMapMu              sync.RWMutex
	allCoverprofileMap     map[string]*CoverEntry
	allCoverprofileMapKeys []string
	allCoverprofileMapMu   sync.RWMutex
)

func SetGIDFunc(fn func() uint64) bool {
	gidFnOnce.Do(func() {
		gidFn = fn
	})
	return true
}

func currentGID() uint64 {
	if gidFn == nil {
		return 0
	}
	return gidFn()
}

func getG(gid uint64) *TraceG {
	gMapMu.RLock()
	defer gMapMu.RUnlock()
	return gMap[gid]
}

func setG(gid uint64, g *TraceG) {
	gMapMu.Lock()
	gMap[gid] = g
	gMapMu.Unlock()
}

func getBlock(blockID string) *Block {
	blockMapMu.RLock()
	defer blockMapMu.RUnlock()

	return blockMap[blockID]
}

func Trace(fileName string, pgid, gid uint64, blockIdx, startLine, endLine, startCol, endCol, numStmts int) {
	isEnabledTraceMu.RLock()
	if !isEnabledTrace {
		isEnabledTraceMu.RUnlock()
		return
	}
	isEnabledTraceMu.RUnlock()

	blockIDStr := blockID(fileName, blockIdx)

	g := getG(gid)
	if g == nil {
		g = newTraceG(gid)
		setG(gid, g)
		if parent := getG(pgid); parent != nil {
			parent.linkG(g)
		}
	}
	g.addCounter(blockIDStr)
}

type Metadata struct {
	FileName   string
	PkgPath    string
	PkgName    string
	ModulePath string
	Funcs      []*Function
}

type Function struct {
	Name    string
	Lit     bool
	Blocks  []*Block
	Deps    []string
	DepRefs []*Function `json:"-"`
}

type Block struct {
	FileName string
	Idx      int
	Start    Pos
	End      Pos
	NumStmts int
	Function *Function `json:"-"`
}

var initOnce sync.Once

func init() {
	initMap()
}

func initMap() {
	initOnce.Do(func() {
		entryMapMu.Lock()
		gMapMu.Lock()
		blockMapMu.Lock()
		funcMapMu.Lock()
		allCoverprofileMapMu.Lock()
		defer entryMapMu.Unlock()
		defer gMapMu.Unlock()
		defer blockMapMu.Unlock()
		defer funcMapMu.Unlock()
		defer allCoverprofileMapMu.Unlock()

		entryMap = make(map[string]*TraceEntry)
		gMap = make(map[uint64]*TraceG)
		blockMap = make(map[string]*Block)
		funcMap = make(map[string]*Function)
		allCoverprofileMap = make(map[string]*CoverEntry)
	})
}

func AddCoverMeta(s string) bool {
	initMap()
	var md Metadata
	if err := json.Unmarshal([]byte(s), &md); err != nil {
		panic(err)
	}
	allCoverprofileMapMu.Lock()

	funcMapMu.Lock()
	for _, fn := range md.Funcs {
		funcMap[fn.Name] = fn
		funcNames = append(funcNames, fn.Name)
	}
	funcMapMu.Unlock()

	for _, fn := range md.Funcs {
		for _, block := range fn.Blocks {
			bid := blockID(md.FileName, block.Idx)
			block.FileName = md.FileName
			block.Function = fn

			blockMapMu.Lock()
			blockMap[bid] = block
			blockMapMu.Unlock()

			allCoverprofileMap[bid] = &CoverEntry{
				FileName:  md.FileName,
				StartLine: block.Start.Line,
				StartCol:  block.Start.Col,
				EndLine:   block.End.Line,
				EndCol:    block.End.Col,
				NumStmts:  block.NumStmts,
			}
			allCoverprofileMapKeys = append(allCoverprofileMapKeys, bid)
		}
	}
	allCoverprofileMapMu.Unlock()

	mdMu.Lock()
	mds = append(mds, &md)
	mdMu.Unlock()

	return true
}

func toEntries(coverMap map[string]*CoverEntry) []*CoverEntry {
	entries := make([]*CoverEntry, 0, len(coverMap))
	for _, key := range allCoverprofileMapKeys {
		entry, exists := coverMap[key]
		if !exists {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func renderMap(mode string, coverMap map[string]*CoverEntry) string {
	b := bytes.NewBuffer([]byte(fmt.Sprintf("mode: %s\n", mode)))
	for _, key := range allCoverprofileMapKeys {
		value, exists := coverMap[key]
		if !exists {
			continue
		}
		_, _ = fmt.Fprint(b, value.String()+"\n")
	}
	return b.String()
}

func blockID(fileName string, blockIdx int) string {
	return fmt.Sprintf("%s:%d", fileName, blockIdx)
}

// CoverReportData holds compact coverage data built from internal state.
type CoverReportData struct {
	Files  []string
	Entry  []string
	All    [][]int
	Counts []CoverReportCountData
}

// CoverReportCountData holds a test name and its coverage entries.
type CoverReportCountData struct {
	Name         string
	Coverprofile [][]int
}

// CollectCoverReportData builds compact coverage data from the current
// internal state (allCoverprofileMap + CoverEntriesMap).
func CollectCoverReportData() *CoverReportData {
	allCoverprofileMapMu.RLock()
	defer allCoverprofileMapMu.RUnlock()

	// Collect unique file names and build file index.
	fileSet := make(map[string]int)
	var files []string
	for _, key := range allCoverprofileMapKeys {
		e := allCoverprofileMap[key]
		if _, ok := fileSet[e.FileName]; !ok {
			fileSet[e.FileName] = len(files)
			files = append(files, e.FileName)
		}
	}
	sort.Strings(files)
	// Rebuild index after sorting.
	for i, f := range files {
		fileSet[f] = i
	}

	// Build block key to index map and "all" array.
	type blockKey struct {
		fileIdx  int
		startLine, startCol, endLine, endCol, numStmts int
	}
	blockIndex := make(map[blockKey]int)
	var all [][]int
	for _, key := range allCoverprofileMapKeys {
		e := allCoverprofileMap[key]
		bk := blockKey{
			fileIdx:   fileSet[e.FileName],
			startLine: e.StartLine,
			startCol:  e.StartCol,
			endLine:   e.EndLine,
			endCol:    e.EndCol,
			numStmts:  e.NumStmts,
		}
		if _, ok := blockIndex[bk]; !ok {
			blockIndex[bk] = len(all)
			all = append(all, []int{bk.fileIdx, bk.startLine, bk.startCol, bk.endLine, bk.endCol, bk.numStmts})
		}
	}

	// Build per-test counts.
	entriesMap := CoverEntriesMap()
	testNames := make([]string, 0, len(entriesMap))
	for name := range entriesMap {
		testNames = append(testNames, name)
	}
	sort.Strings(testNames)

	counts := make([]CoverReportCountData, 0, len(testNames))
	for _, name := range testNames {
		entries := entriesMap[name]
		profile := make([][]int, 0, len(entries))
		for _, e := range entries {
			bk := blockKey{
				fileIdx:   fileSet[e.FileName],
				startLine: e.StartLine,
				startCol:  e.StartCol,
				endLine:   e.EndLine,
				endCol:    e.EndCol,
				numStmts:  e.NumStmts,
			}
			idx, ok := blockIndex[bk]
			if !ok {
				continue
			}
			profile = append(profile, []int{idx, e.Count})
		}
		counts = append(counts, CoverReportCountData{
			Name:         name,
			Coverprofile: profile,
		})
	}

	return &CoverReportData{
		Files: files,
		Entry: []string{"file", "startLine", "startCol", "endLine", "endCol", "stmts"},
		All:   all,
		Counts: counts,
	}
}

// MarshalCoverJSON marshals the compact coverage report as JSON.
// The output matches the tobari.CoverReport structure with nested metadata.
func MarshalCoverJSON() ([]byte, error) {
	data := CollectCoverReportData()
	type jsonMetadata struct {
		Files []string `json:"files"`
		Entry []string `json:"entry"`
		All   [][]int  `json:"all"`
	}
	type jsonCount struct {
		Name         string  `json:"name"`
		Coverprofile [][]int `json:"coverprofile"`
	}
	type jsonReport struct {
		Metadata jsonMetadata `json:"metadata"`
		Counts   []jsonCount  `json:"counts"`
	}
	counts := make([]jsonCount, len(data.Counts))
	for i, c := range data.Counts {
		counts[i] = jsonCount{
			Name:         c.Name,
			Coverprofile: c.Coverprofile,
		}
	}
	return json.Marshal(jsonReport{
		Metadata: jsonMetadata{
			Files: data.Files,
			Entry: data.Entry,
			All:   data.All,
		},
		Counts: counts,
	})
}

// MarshalCoverTOON marshals the compact coverage report in human-readable TOON format.
func MarshalCoverTOON() ([]byte, error) {
	return MarshalReportDataTOON(CollectCoverReportData())
}

// MarshalReportDataTOON renders CoverReportData in human-readable TOON format.
func MarshalReportDataTOON(data *CoverReportData) ([]byte, error) {
	var buf bytes.Buffer
	names := make([]string, len(data.Counts))
	for i, c := range data.Counts {
		names[i] = c.Name
	}
	sort.Strings(names)

	countsByName := make(map[string]CoverReportCountData, len(data.Counts))
	for _, c := range data.Counts {
		countsByName[c.Name] = c
	}

	for _, name := range names {
		c := countsByName[name]
		fmt.Fprintf(&buf, "%s[%d]{FileName,StartLine,StartCol,EndLine,EndCol,StatementCount,Count}:\n", name, len(c.Coverprofile))
		for _, cp := range c.Coverprofile {
			if len(cp) != 2 {
				continue
			}
			blockIdx := cp[0]
			count := cp[1]
			if blockIdx < 0 || blockIdx >= len(data.All) {
				continue
			}
			block := data.All[blockIdx]
			if len(block) != 6 {
				continue
			}
			fileIdx := block[0]
			fileName := ""
			if fileIdx >= 0 && fileIdx < len(data.Files) {
				fileName = data.Files[fileIdx]
			}
			fmt.Fprintf(&buf, "\t%s,%d,%d,%d,%d,%d,%d\n", fileName, block[1], block[2], block[3], block[4], block[5], count)
		}
	}
	return buf.Bytes(), nil
}

func EncodeMeta() ([]byte, error) {
	mdMu.RLock()
	defer mdMu.RUnlock()

	return json.Marshal(mds)
}

type CoverageFuncCounter struct {
	Counters []uint32
	Len      uint64
}

func EncodeCounters() ([]byte, error) {
	mdMu.RLock()
	gMapMu.RLock()
	defer mdMu.RUnlock()
	defer gMapMu.RUnlock()

	blockToCountMap := make(map[string]int)
	for _, g := range gMap {
		g.blockToCountMap(blockToCountMap)
	}

	var allFnCounters []CoverageFuncCounter
	for mdIdx, md := range mds {
		var flatCounters []uint32

		// Calculate total size needed: for each function we need
		// firstCtr + nCtrs counters, where firstCtr contains [nCtrs, pkgId+1, funcId]
		for fnIdx, fn := range md.Funcs {
			// Add function header at the first counter position
			flatCounters = append(flatCounters, uint32(len(fn.Blocks))) // NumCtrsOffset = 0
			flatCounters = append(flatCounters, uint32(mdIdx)+1)        // PkgIdOffset = 1 (use metadata index, add 1 as per Go source)
			flatCounters = append(flatCounters, uint32(fnIdx))          // FuncIdOffset = 2 (function index in metadata)

			// Add actual counter values at FirstCtrOffset onwards
			for _, block := range fn.Blocks {
				bid := blockID(block.FileName, block.Idx)
				cnt := blockToCountMap[bid]
				// For "set" mode, use 1 if counter > 0, else 0
				var ctrVal uint32 = 0
				if cnt > 0 {
					ctrVal = 1
				}
				flatCounters = append(flatCounters, ctrVal)
			}
		}

		allFnCounters = append(allFnCounters, CoverageFuncCounter{
			Counters: flatCounters,
			Len:      uint64(len(flatCounters)),
		})
	}
	return json.Marshal(allFnCounters)
}

var (
	embeddedSourcesMu sync.Mutex
	embeddedSourceMap map[string]string // original path → gzip-compressed content (rodata string)
)

// AddEmbeddedSource is called via go:linkname from each instrumented package's
// covervars.go at init time (same timing as AddCoverMeta).
// origPath is the original absolute path of the source file.
// content is a gzip-compressed string stored in rodata.
func AddEmbeddedSource(origPath string, content string) bool {
	embeddedSourcesMu.Lock()
	if embeddedSourceMap == nil {
		embeddedSourceMap = make(map[string]string)
	}
	embeddedSourceMap[origPath] = content
	embeddedSourcesMu.Unlock()
	return true
}

// GetEmbeddedSources returns all embedded source files as a map of
// original path → gzip-compressed content. Returns nil if no sources were embedded.
func GetEmbeddedSources() map[string]string {
	embeddedSourcesMu.Lock()
	defer embeddedSourcesMu.Unlock()
	return embeddedSourceMap
}

// ExtractEmbeddedSourcesIfRequested checks the TOBARI_EXTRACT_SOURCES environment
// variable. If set, it writes all embedded sources as a tar.gz archive to the
// specified path and exits the process. This is called via go:linkname from the
// main package's init function when the binary was built with --embed-code.
func ExtractEmbeddedSourcesIfRequested() {
	outputPath := os.Getenv("TOBARI_EXTRACT_SOURCES")
	if outputPath == "" {
		return
	}

	embeddedSourcesMu.Lock()
	sources := embeddedSourceMap
	embeddedSourcesMu.Unlock()

	if len(sources) == 0 {
		fmt.Fprintln(os.Stderr, "tobari: no embedded sources found")
		os.Exit(1)
	}

	if err := writeEmbeddedSourcesArchive(outputPath, sources); err != nil {
		fmt.Fprintf(os.Stderr, "tobari: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func writeEmbeddedSourcesArchive(outputPath string, sources map[string]string) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", outputPath, err)
	}
	defer func() {
		_ = f.Close()
	}()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	for origPath, compressed := range sources {
		gr, err := gzip.NewReader(strings.NewReader(compressed))
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %s: %w", origPath, err)
		}
		content, err := io.ReadAll(gr)
		if err != nil {
			return fmt.Errorf("failed to read content from %s: %w", origPath, err)
		}
		if err := gr.Close(); err != nil {
			return fmt.Errorf("failed to close: %s: %w", origPath, err)
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: filepath.ToSlash(origPath),
			Mode: 0o600,
			Size: int64(len(content)),
		}); err != nil {
			return fmt.Errorf("failed to write header: %s: %w", origPath, err)
		}
		if _, err := tw.Write(content); err != nil {
			return fmt.Errorf("failed to write content: %s: %w", origPath, err)
		}
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("failed to close tar writer: %w", err)
	}
	if err := gw.Close(); err != nil {
		return fmt.Errorf("failed to close gzip writer: %w", err)
	}
	return nil
}
