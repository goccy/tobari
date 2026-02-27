package tobari

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/goccy/tobari/internal/tobari"
)

// Mode corresponds to the mode in the coverprofile format.
type Mode string

const (
	// SetMode represents `set` mode.
	SetMode Mode = "set"
	// CountMode represents `count` mode.
	CountMode Mode = "count"
	// AtomicMode represents `atomic` mode.
	AtomicMode Mode = "atomic"
)

// EnableCoverageCounting enables the counting functionality necessary for calculating coverage.
// Since it is enabled by default, it is assumed that this function is called after DisableCoverageCounting has been invoked.
func EnableCoverageCounting() {
	tobari.EnableCoverageCounting()
}

// DisableCoverageCounting disables the counting functionality required for calculating coverage.
// Since it is enabled by default, this is used in scenarios such as production environments where you want to disable the counting logic to avoid performance impact.
func DisableCoverageCounting() {
	tobari.DisableCoverageCounting()
}

// ClearCounters similar to ClearCounters in runtime/coverage, this resets the currently active counters.
// It is intended to be called at the start of coverage measurement.
func ClearCounters() {
	tobari.ClearCounters()
}

// Cover this feature is used to scoped coverage measurement.
// While `runtime/coverage` measures coverage across entire specified packages,
// using this feature allows coverage to be measured for specific functions.
// For example, by implementing this feature in an HTTP server middleware or a gRPC server interceptor,
// you can measure coverage on a per-method basis for each request.
func Cover(fn func()) {
	cover("", fn)
}

// CoverWithName when measuring coverage, you can assign a name to the measurement scope.
// By using the WriteCoverprofileByName method when writing out the results,
// you can filter and output only the coverage data associated with the named measurement target.
func CoverWithName(name string, fn func()) {
	cover(name, fn)
}

func cover(name string, fn func()) {
	// Identifies the source code positions where Cover or CoverWithName is being used.
	_, file, line, _ := runtime.Caller(2)
	scopeID := fmt.Sprintf("%s:%s:%d", name, file, line)
	waitCh := make(chan error)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				waitCh <- fmt.Errorf("tobari: recovered: %v", r)
			}
		}()

		tobari.Cover(name, scopeID)
		fn()
		waitCh <- nil
	}()
	if err := <-waitCh; err != nil {
		// Re-throws the panic that occurred inside fn().
		panic(err)
	}
}

// WriteAllCoverprofile uses the coverage counts at the time this method is called to write coverage data for all locations subject to coverage measurement to the provided io.Writer value.
// Since the data is written in the coverprofile format, you can directly use with `go tool cover`.
// The resulting data is the same as the data obtained by decoding the output from executing WriteMeta and WriteCounters using runtime/coverage.
func WriteAllCoverprofile(mode Mode, w io.Writer) {
	tobari.WriteAllCoverprofile(string(mode), w)
}

// WriteCoverprofile writes coverprofile data based on the coverage range measured by Cover or CoverWithName.
// Parts that are not invoked via the Cover or CoverWithName methods are not counted.
// Also, unreachable ranges are calculated based on the paths that were actually called.
func WriteCoverprofile(mode Mode, w io.Writer) {
	tobari.WriteCoverprofile(string(mode), w)
}

// WriteCoverprofileByName it basically works the same as WriteCoverprofile,
// but additionally allows you to target only the ranges with the specified name.
func WriteCoverprofileByName(name string, mode Mode, w io.Writer) {
	tobari.WriteCoverprofileByName(name, string(mode), w)
}

// Coverprofile represents coverage data in coverprofile format.
type Coverprofile struct {
	// Mode indicates the coverage mode used.
	Mode Mode
	// Entries contains the list of coverage entries.
	Entries []*Entry
}

// Entry represents a coverage entry for a source file.
type Entry struct {
	// FileName is the name of the source file.
	FileName string
	// Start is the starting position of the covered entry.
	Start EntryPos
	// End is the ending position of the covered entry.
	End EntryPos
	// StatementCount is the number of statements in the covered entry.
	StatementCount int
	// Count is the number of times the entry was covered.
	Count int
}

// EntryPos represents a position in a source file.
type EntryPos struct {
	// Line number in the source file.
	Line int
	// Column number in the source file.
	Column int
}

// CoverprofileByName retrieve coverage data for the specified name.
func CoverprofileByName(name string, mode Mode) *Coverprofile {
	return &Coverprofile{
		Mode:    mode,
		Entries: toEntries(tobari.CoverEntriesByName(name)),
	}
}

// CoverprofileMap if coverage was measured using CoverWithName,
// it outputs the correspondence between the names and the coverprofile data for each name.
func CoverprofileMap(mode Mode) map[string]*Coverprofile {
	entriesMap := tobari.CoverEntriesMap()
	coverprofMap := make(map[string]*Coverprofile, len(entriesMap))
	for name, entries := range entriesMap {
		coverprofMap[name] = &Coverprofile{
			Mode:    mode,
			Entries: toEntries(entries),
		}
	}
	return coverprofMap
}

func toEntries(e []*tobari.CoverEntry) []*Entry {
	ret := make([]*Entry, 0, len(e))
	for _, ee := range e {
		ret = append(ret, toEntry(ee))
	}
	return ret
}

func toEntry(e *tobari.CoverEntry) *Entry {
	return &Entry{
		FileName: e.FileName,
		Start: EntryPos{
			Line:   e.StartLine,
			Column: e.StartCol,
		},
		End: EntryPos{
			Line:   e.EndLine,
			Column: e.EndCol,
		},
		StatementCount: e.NumStmts,
		Count:          e.Count,
	}
}

// CoverReport holds per-test coverage data in a compact format.
// The struct mirrors the tobari.json schema directly, so json.Marshal
// produces the compact format without custom marshaling.
type CoverReport struct {
	Metadata  CoverReportMetadata `json:"metadata"`
	Counts    []*CoverReportCount `json:"counts"`
	AllCounts []int               `json:"allcounts,omitempty"`
}

// CoverReportMetadata contains file names, entry column definitions,
// and all instrumented block definitions.
type CoverReportMetadata struct {
	Files []string `json:"files"`
	Entry []string `json:"entry"`
	All   [][]int  `json:"all"`
}

// CoverReportCount holds a test name and its coverage entries.
type CoverReportCount struct {
	Name         string  `json:"name"`
	Coverprofile [][]int `json:"coverprofile"`
}

// CollectCoverReport collects the current coverage data measured by
// CoverWithName and returns it as a CoverReport.
func CollectCoverReport() *CoverReport {
	data := tobari.CollectCoverReportData()
	counts := make([]*CoverReportCount, len(data.Counts))
	for i, c := range data.Counts {
		counts[i] = &CoverReportCount{
			Name:         c.Name,
			Coverprofile: c.Coverprofile,
		}
	}
	return &CoverReport{
		Metadata: CoverReportMetadata{
			Files: data.Files,
			Entry: data.Entry,
			All:   data.All,
		},
		Counts:    counts,
		AllCounts: data.AllCounts,
	}
}

// WriteCoverprofile writes the merged coverage data in coverprofile format.
// All blocks from Metadata.All are included; blocks not covered by any test
// appear with count=0. If AllCounts is present, it is used directly;
// otherwise counts from all tests are summed per block.
func (r *CoverReport) WriteCoverprofile(w io.Writer) error {
	type blockEntry struct {
		key            string
		statementCount int
		count          int
	}

	// Initialize all blocks from metadata.all with count=0.
	blocks := make([]*blockEntry, 0, len(r.Metadata.All))
	blockByIdx := make(map[int]*blockEntry, len(r.Metadata.All))
	for i, block := range r.Metadata.All {
		if len(block) != 6 {
			continue
		}
		fileIdx := block[0]
		if fileIdx < 0 || fileIdx >= len(r.Metadata.Files) {
			continue
		}
		key := fmt.Sprintf("%s:%d.%d,%d.%d",
			r.Metadata.Files[fileIdx],
			block[1], block[2], block[3], block[4])
		entry := &blockEntry{key: key, statementCount: block[5]}
		blocks = append(blocks, entry)
		blockByIdx[i] = entry
	}

	if len(r.AllCounts) == len(r.Metadata.All) {
		for i, count := range r.AllCounts {
			if entry, ok := blockByIdx[i]; ok {
				entry.count = count
			}
		}
	} else {
		// Overlay counts from each test.
		for _, c := range r.Counts {
			for _, cp := range c.Coverprofile {
				if len(cp) != 2 {
					continue
				}
				if entry, ok := blockByIdx[cp[0]]; ok {
					entry.count += cp[1]
				}
			}
		}
	}

	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].key < blocks[j].key
	})

	if _, err := io.WriteString(w, "mode: set\n"); err != nil {
		return err
	}
	for _, entry := range blocks {
		if _, err := fmt.Fprintf(w, "%s %d %d\n", entry.key, entry.statementCount, entry.count); err != nil {
			return err
		}
	}
	return nil
}

// MarshalTOON produces human-readable TOON format from the CoverReport.
func (r *CoverReport) MarshalTOON() ([]byte, error) {
	counts := make([]tobari.CoverReportCountData, len(r.Counts))
	for i, c := range r.Counts {
		counts[i] = tobari.CoverReportCountData{
			Name:         c.Name,
			Coverprofile: c.Coverprofile,
		}
	}
	return tobari.MarshalReportDataTOON(&tobari.CoverReportData{
		Files:  r.Metadata.Files,
		Entry:  r.Metadata.Entry,
		All:    r.Metadata.All,
		Counts: counts,
	})
}

// MergeCoverReports merges multiple CoverReport values into a single unified report.
// File lists and block definitions are unified with deduplication, block indices
// in Counts are remapped accordingly, and AllCounts are summed per block.
func MergeCoverReports(reports []*CoverReport) (*CoverReport, error) {
	if len(reports) == 0 {
		return nil, fmt.Errorf("no reports to merge")
	}

	// Build unified sorted file list.
	fileSet := make(map[string]struct{})
	for _, r := range reports {
		for _, f := range r.Metadata.Files {
			fileSet[f] = struct{}{}
		}
	}
	unifiedFiles := make([]string, 0, len(fileSet))
	for f := range fileSet {
		unifiedFiles = append(unifiedFiles, f)
	}
	sort.Strings(unifiedFiles)

	fileIndex := make(map[string]int, len(unifiedFiles))
	for i, f := range unifiedFiles {
		fileIndex[f] = i
	}

	// Build unified block list with deduplication.
	type blockKey struct {
		fileIdx, startLine, startCol, endLine, endCol, numStmts int
	}
	blockMap := make(map[blockKey]int)
	var unifiedAll [][]int

	// Per-report block index mapping (old block idx -> new block idx).
	blockMaps := make([]map[int]int, len(reports))
	for i, r := range reports {
		blockMaps[i] = make(map[int]int, len(r.Metadata.All))
		for j, block := range r.Metadata.All {
			if len(block) != 6 {
				continue
			}
			oldFileIdx := block[0]
			if oldFileIdx < 0 || oldFileIdx >= len(r.Metadata.Files) {
				continue
			}
			key := blockKey{
				fileIdx:   fileIndex[r.Metadata.Files[oldFileIdx]],
				startLine: block[1],
				startCol:  block[2],
				endLine:   block[3],
				endCol:    block[4],
				numStmts:  block[5],
			}
			if newIdx, ok := blockMap[key]; ok {
				blockMaps[i][j] = newIdx
			} else {
				newIdx := len(unifiedAll)
				blockMap[key] = newIdx
				unifiedAll = append(unifiedAll, []int{
					key.fileIdx, key.startLine, key.startCol,
					key.endLine, key.endCol, key.numStmts,
				})
				blockMaps[i][j] = newIdx
			}
		}
	}

	// Remap and concatenate counts.
	mergedCounts := make([]*CoverReportCount, 0)
	for i, r := range reports {
		for _, c := range r.Counts {
			newProfile := make([][]int, 0, len(c.Coverprofile))
			for _, cp := range c.Coverprofile {
				if len(cp) != 2 {
					continue
				}
				if newIdx, ok := blockMaps[i][cp[0]]; ok {
					newProfile = append(newProfile, []int{newIdx, cp[1]})
				}
			}
			mergedCounts = append(mergedCounts, &CoverReportCount{
				Name:         c.Name,
				Coverprofile: newProfile,
			})
		}
	}

	// Compute AllCounts. For each report, use AllCounts if available,
	// otherwise sum from individual Counts.
	allCounts := make([]int, len(unifiedAll))
	for i, r := range reports {
		if len(r.AllCounts) == len(r.Metadata.All) {
			for oldIdx, count := range r.AllCounts {
				if newIdx, ok := blockMaps[i][oldIdx]; ok {
					allCounts[newIdx] += count
				}
			}
		} else {
			for _, c := range r.Counts {
				for _, cp := range c.Coverprofile {
					if len(cp) != 2 {
						continue
					}
					if newIdx, ok := blockMaps[i][cp[0]]; ok {
						allCounts[newIdx] += cp[1]
					}
				}
			}
		}
	}

	return &CoverReport{
		Metadata: CoverReportMetadata{
			Files: unifiedFiles,
			Entry: reports[0].Metadata.Entry,
			All:   unifiedAll,
		},
		Counts:    mergedCounts,
		AllCounts: allCounts,
	}, nil
}

// ReadCoverArchivedFile extracts the original source files embedded during
// coverage instrumentation and returns them as a tar.gz archive.
// Returns nil if no sources were embedded.
// Each embedded source is stored as a gzip-compressed const string in rodata.
// Decompression and tar construction are streamed via io.Pipe so that
// only one file's content is in memory at a time.
// Errors during streaming are reported through the returned io.Reader.
func ReadCoverArchivedFile() io.Reader {
	sources := tobari.GetEmbeddedSources()
	if len(sources) == 0 {
		return nil
	}

	// Sort paths for deterministic (idempotent) tar.gz output.
	paths := make([]string, 0, len(sources))
	for p := range sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	pr, pw := io.Pipe()
	go func() {
		gw := gzip.NewWriter(pw)
		tw := tar.NewWriter(gw)

		for _, origPath := range paths {
			compressed := sources[origPath]
			// Decompress from rodata string via strings.NewReader (zero-copy)
			if err := func() error {
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
				return nil
			}(); err != nil {
				pw.CloseWithError(err)
				return
			}
		}

		pw.CloseWithError(errors.Join(tw.Close(), gw.Close()))
	}()
	return pr
}

// MergeCoverArchivedFiles merges multiple tar.gz source archives into a single archive.
// Duplicate archives (identical SHA-256 hash) are automatically skipped.
// If the same file path appears in multiple archives with different content,
// an error is returned. The output is deterministic: entries are sorted by path
// and gzip metadata is fixed, so identical inputs always produce identical output.
func MergeCoverArchivedFiles(inputs []io.Reader, w io.Writer) error {
	if len(inputs) == 0 {
		return fmt.Errorf("no inputs to merge")
	}

	// Read all inputs and deduplicate by SHA-256 hash.
	seen := make(map[[sha256.Size]byte]struct{})
	var uniqueData [][]byte
	for _, r := range inputs {
		data, err := io.ReadAll(r)
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}
		hash := sha256.Sum256(data)
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		uniqueData = append(uniqueData, data)
	}

	// Extract all unique archives and detect conflicts.
	type fileEntry struct {
		content []byte
		hash    [sha256.Size]byte
	}
	files := make(map[string]*fileEntry)

	for _, data := range uniqueData {
		gr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		tr := tar.NewReader(gr)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("failed to read tar entry: %w", err)
			}
			content, err := io.ReadAll(tr)
			if err != nil {
				return fmt.Errorf("failed to read content for %s: %w", hdr.Name, err)
			}
			contentHash := sha256.Sum256(content)
			if existing, ok := files[hdr.Name]; ok {
				if existing.hash != contentHash {
					return fmt.Errorf("conflict: file %q has different content in source archives", hdr.Name)
				}
				continue
			}
			files[hdr.Name] = &fileEntry{content: content, hash: contentHash}
		}
		_ = gr.Close()
	}

	// Sort paths for deterministic output.
	sortedPaths := make([]string, 0, len(files))
	for p := range files {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	// Write merged tar.gz with deterministic gzip header.
	gw := gzip.NewWriter(w)
	gw.Header.ModTime = time.Unix(0, 0)
	tw := tar.NewWriter(gw)
	for _, p := range sortedPaths {
		entry := files[p]
		if err := tw.WriteHeader(&tar.Header{
			Name: p,
			Mode: 0o600,
			Size: int64(len(entry.content)),
		}); err != nil {
			return fmt.Errorf("failed to write header for %s: %w", p, err)
		}
		if _, err := tw.Write(entry.content); err != nil {
			return fmt.Errorf("failed to write content for %s: %w", p, err)
		}
	}
	return errors.Join(tw.Close(), gw.Close())
}
