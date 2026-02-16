package cli

import (
	"path/filepath"
	"sort"
	"strings"
)

// viewData is the top-level structure serialized as JSON into the HTML.
type viewData struct {
	Tests      []*viewTest    `json:"tests"`
	TestTree   []*testNode    `json:"testTree"`
	Files      []*viewFile    `json:"files"`
	Overlaps   []*overlapPair `json:"overlaps"`
	InstrLines map[int][]int  `json:"instrLines"` // fileIndex → all instrumented line numbers (Count >= 0)
}

// viewTest represents a test with line-level coverage (compact representation).
type viewTest struct {
	Name     string        `json:"n"`
	Coverage map[int][]int `json:"c"` // fileIndex → sorted list of covered line numbers
}

// viewFile represents a source file with its content.
type viewFile struct {
	Path  string   `json:"path"`
	Short string   `json:"short"`
	Lines []string `json:"lines"`
}

// testNode represents a node in the test name tree.
type testNode struct {
	Name     string      `json:"name"`
	FullName string      `json:"fullName"`
	IsLeaf   bool        `json:"isLeaf"`
	Children []*testNode `json:"children,omitempty"`
}

// overlapPair stores the overlap metric between two tests.
type overlapPair struct {
	TestA     string  `json:"testA"`
	TestB     string  `json:"testB"`
	MatchRate float64 `json:"matchRate"`
	Common    int     `json:"common"`
	Total     int     `json:"total"`
}

// buildTestTree constructs a hierarchical tree from test names split by "/".
func buildTestTree(testNames []string) []*testNode {
	sort.Strings(testNames)

	root := &testNode{Children: make([]*testNode, 0)}

	for _, name := range testNames {
		parts := strings.Split(name, "/")
		current := root
		for i, part := range parts {
			fullName := strings.Join(parts[:i+1], "/")
			child := findChild(current, part)
			if child == nil {
				child = &testNode{
					Name:     part,
					FullName: fullName,
					Children: make([]*testNode, 0),
				}
				current.Children = append(current.Children, child)
			}
			if i == len(parts)-1 {
				child.IsLeaf = true
			}
			current = child
		}
	}

	return root.Children
}

func findChild(parent *testNode, name string) *testNode {
	for _, c := range parent.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// convertToLineCoverage converts raw tobariJSONEntry entries to a line-level coverage map.
// Returns fileIndex → sorted list of covered line numbers.
func convertToLineCoverage(entries []tobariJSONEntry, fileIndexMap map[string]int) map[int][]int {
	coveredLines := make(map[int]map[int]struct{})

	for _, e := range entries {
		if e.Count <= 0 {
			continue
		}
		fi, ok := fileIndexMap[e.FileName]
		if !ok {
			continue
		}
		if coveredLines[fi] == nil {
			coveredLines[fi] = make(map[int]struct{})
		}
		for line := e.Start.Line; line <= e.End.Line; line++ {
			coveredLines[fi][line] = struct{}{}
		}
	}

	result := make(map[int][]int, len(coveredLines))
	for fi, lines := range coveredLines {
		sorted := make([]int, 0, len(lines))
		for line := range lines {
			sorted = append(sorted, line)
		}
		sort.Ints(sorted)
		result[fi] = sorted
	}
	return result
}

// computeOverlaps computes Jaccard similarity between top-level tests.
// Top-level tests are determined by the first segment before "/".
// Child test coverages are merged into their top-level parent.
func computeOverlaps(entriesMap map[string][]tobariJSONEntry, fileIndexMap map[string]int) []*overlapPair {
	// Group entries by top-level test name
	type sigKey struct {
		fi        int
		startLine int
		startCol  int
		endLine   int
		endCol    int
		covered   int
	}

	topLevelSigs := make(map[string]map[sigKey]struct{})

	for testName, entries := range entriesMap {
		topLevel := strings.SplitN(testName, "/", 2)[0]
		if topLevelSigs[topLevel] == nil {
			topLevelSigs[topLevel] = make(map[sigKey]struct{})
		}
		for _, e := range entries {
			fi, ok := fileIndexMap[e.FileName]
			if !ok {
				continue
			}
			covered := 0
			if e.Count > 0 {
				covered = 1
			}
			key := sigKey{
				fi:        fi,
				startLine: e.Start.Line,
				startCol:  e.Start.Column,
				endLine:   e.End.Line,
				endCol:    e.End.Column,
				covered:   covered,
			}
			topLevelSigs[topLevel][key] = struct{}{}
		}
	}

	// Collect sorted top-level test names
	var topLevelNames []string
	for name := range topLevelSigs {
		topLevelNames = append(topLevelNames, name)
	}
	sort.Strings(topLevelNames)

	// Compute pairwise Jaccard similarity
	var pairs []*overlapPair
	for i := 0; i < len(topLevelNames); i++ {
		sigsA := topLevelSigs[topLevelNames[i]]
		for j := i + 1; j < len(topLevelNames); j++ {
			sigsB := topLevelSigs[topLevelNames[j]]

			common := 0
			for sig := range sigsA {
				if _, ok := sigsB[sig]; ok {
					common++
				}
			}
			total := len(sigsA) + len(sigsB) - common
			if total == 0 {
				continue
			}
			rate := float64(common) / float64(total)
			pairs = append(pairs, &overlapPair{
				TestA:     topLevelNames[i],
				TestB:     topLevelNames[j],
				MatchRate: rate,
				Common:    common,
				Total:     total,
			})
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].MatchRate > pairs[j].MatchRate
	})

	return pairs
}

// shortenFilePaths removes the common directory prefix from file paths.
func shortenFilePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	if len(paths) == 1 {
		return []string{filepath.Base(paths[0])}
	}

	// Find common prefix
	prefix := filepath.Dir(paths[0])
	for _, p := range paths[1:] {
		dir := filepath.Dir(p)
		for !strings.HasPrefix(dir, prefix) && prefix != "" {
			prefix = filepath.Dir(prefix)
		}
	}
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	result := make([]string, len(paths))
	for i, p := range paths {
		short := strings.TrimPrefix(p, prefix)
		if short == "" {
			short = filepath.Base(p)
		}
		result[i] = short
	}
	return result
}

// collectUncoveredLines determines which lines in each file have coverage entries
// but are not covered by the given entries.
func collectAllCoverageLines(entriesMap map[string][]tobariJSONEntry, fileIndexMap map[string]int) map[int]map[int]struct{} {
	allLines := make(map[int]map[int]struct{})
	for _, entries := range entriesMap {
		for _, e := range entries {
			fi, ok := fileIndexMap[e.FileName]
			if !ok {
				continue
			}
			if allLines[fi] == nil {
				allLines[fi] = make(map[int]struct{})
			}
			for line := e.Start.Line; line <= e.End.Line; line++ {
				allLines[fi][line] = struct{}{}
			}
		}
	}
	return allLines
}

// buildViewData constructs the viewData from parsed tobari.json entries and source files.
func buildViewData(entriesMap map[string][]tobariJSONEntry, files []*viewFile, fileIndexMap map[string]int) *viewData {
	// Build tests with line-level coverage
	var testNames []string
	for name := range entriesMap {
		testNames = append(testNames, name)
	}
	sort.Strings(testNames)

	tests := make([]*viewTest, 0, len(testNames))
	for _, name := range testNames {
		entries := entriesMap[name]
		cov := convertToLineCoverage(entries, fileIndexMap)
		// Only include non-empty coverage
		if len(cov) > 0 {
			tests = append(tests, &viewTest{
				Name:     name,
				Coverage: cov,
			})
		} else {
			tests = append(tests, &viewTest{
				Name:     name,
				Coverage: nil,
			})
		}
	}

	// Build test tree
	tree := buildTestTree(testNames)

	// Compute overlaps
	overlaps := computeOverlaps(entriesMap, fileIndexMap)

	// Compute all instrumented lines per file (lines from any entry, regardless of Count)
	allInstrLines := collectAllCoverageLines(entriesMap, fileIndexMap)
	instrLines := make(map[int][]int, len(allInstrLines))
	for fi, lineSet := range allInstrLines {
		sorted := make([]int, 0, len(lineSet))
		for line := range lineSet {
			sorted = append(sorted, line)
		}
		sort.Ints(sorted)
		instrLines[fi] = sorted
	}

	return &viewData{
		Tests:      tests,
		TestTree:   tree,
		Files:      files,
		Overlaps:   overlaps,
		InstrLines: instrLines,
	}
}

// buildFileIndex collects unique file paths from all entries and returns
// the sorted paths and a map from path to index.
func buildFileIndex(entriesMap map[string][]tobariJSONEntry) ([]string, map[string]int) {
	pathSet := make(map[string]struct{})
	for _, entries := range entriesMap {
		for _, e := range entries {
			pathSet[e.FileName] = struct{}{}
		}
	}

	paths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	indexMap := make(map[string]int, len(paths))
	for i, p := range paths {
		indexMap[p] = i
	}
	return paths, indexMap
}

