package tobari

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

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
	Metadata CoverReportMetadata `json:"metadata"`
	Counts   []*CoverReportCount `json:"counts"`
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
		Counts: counts,
	}
}

// WriteCoverprofile writes the merged coverage data in coverprofile format.
// All blocks from Metadata.All are included; blocks not covered by any test
// appear with count=0. Counts from all tests are summed per block.
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

	pr, pw := io.Pipe()
	go func() {
		gw := gzip.NewWriter(pw)
		tw := tar.NewWriter(gw)

		for origPath, compressed := range sources {
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
