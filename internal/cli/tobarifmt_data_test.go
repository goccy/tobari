package cli

import (
	"strings"
	"testing"
)

func TestBuildTestTree(t *testing.T) {
	names := []string{
		"TestAdd",
		"TestDecoder",
		"TestDecoder/hello",
		"TestDecoder/world",
		"TestEncoder",
		"TestEncoder/a",
		"TestEncoder/b",
	}
	tree := buildTestTree(names)

	if len(tree) != 3 {
		t.Fatalf("expected 3 top-level nodes, got %d", len(tree))
	}

	// TestAdd
	if tree[0].Name != "TestAdd" || !tree[0].IsLeaf || len(tree[0].Children) != 0 {
		t.Errorf("TestAdd node: name=%s, isLeaf=%v, children=%d", tree[0].Name, tree[0].IsLeaf, len(tree[0].Children))
	}

	// TestDecoder
	if tree[1].Name != "TestDecoder" || !tree[1].IsLeaf || len(tree[1].Children) != 2 {
		t.Errorf("TestDecoder node: name=%s, isLeaf=%v, children=%d", tree[1].Name, tree[1].IsLeaf, len(tree[1].Children))
	}
	if tree[1].Children[0].Name != "hello" || tree[1].Children[0].FullName != "TestDecoder/hello" {
		t.Errorf("TestDecoder/hello: name=%s, fullName=%s", tree[1].Children[0].Name, tree[1].Children[0].FullName)
	}

	// TestEncoder
	if tree[2].Name != "TestEncoder" || !tree[2].IsLeaf || len(tree[2].Children) != 2 {
		t.Errorf("TestEncoder node: name=%s, isLeaf=%v, children=%d", tree[2].Name, tree[2].IsLeaf, len(tree[2].Children))
	}
}

func TestConvertToLineCoverage(t *testing.T) {
	entries := []tobariJSONEntry{
		{FileName: "a.go", Start: tobariEntryPos{Line: 3, Column: 1}, End: tobariEntryPos{Line: 5, Column: 2}, Count: 1},
		{FileName: "a.go", Start: tobariEntryPos{Line: 7, Column: 1}, End: tobariEntryPos{Line: 8, Column: 2}, Count: 0}, // not covered
		{FileName: "b.go", Start: tobariEntryPos{Line: 10, Column: 1}, End: tobariEntryPos{Line: 12, Column: 2}, Count: 2},
	}
	fileIndexMap := map[string]int{"a.go": 0, "b.go": 1}
	cov := convertToLineCoverage(entries, fileIndexMap)

	// a.go: lines 3,4,5 covered (count=1), lines 7,8 NOT covered (count=0)
	aLines := cov[0]
	if len(aLines) != 3 {
		t.Fatalf("a.go: expected 3 covered lines, got %d: %v", len(aLines), aLines)
	}

	// b.go: lines 10,11,12 covered
	bLines := cov[1]
	if len(bLines) != 3 {
		t.Fatalf("b.go: expected 3 covered lines, got %d: %v", len(bLines), bLines)
	}
}

func TestShortenFilePaths(t *testing.T) {
	paths := []string{
		"/Users/user/project/pkg/decode.go",
		"/Users/user/project/pkg/encode.go",
		"/Users/user/project/internal/scanner.go",
	}
	short := shortenFilePaths(paths)
	if len(short) != 3 {
		t.Fatalf("expected 3 paths, got %d", len(short))
	}
	// Common prefix is /Users/user/project/
	for _, s := range short {
		if strings.HasPrefix(s, "/Users/user/project/") {
			t.Errorf("path %q still has common prefix", s)
		}
	}
}

func TestComputeOverlaps(t *testing.T) {
	entriesMap := map[string][]tobariJSONEntry{
		"TestA": {
			{FileName: "a.go", Start: tobariEntryPos{Line: 1, Column: 1}, End: tobariEntryPos{Line: 3, Column: 2}, StatementCount: 1, Count: 1},
			{FileName: "a.go", Start: tobariEntryPos{Line: 5, Column: 1}, End: tobariEntryPos{Line: 7, Column: 2}, StatementCount: 1, Count: 1},
		},
		"TestB": {
			{FileName: "a.go", Start: tobariEntryPos{Line: 1, Column: 1}, End: tobariEntryPos{Line: 3, Column: 2}, StatementCount: 1, Count: 1},
			{FileName: "a.go", Start: tobariEntryPos{Line: 5, Column: 1}, End: tobariEntryPos{Line: 7, Column: 2}, StatementCount: 1, Count: 0},
		},
	}
	fileIndexMap := map[string]int{"a.go": 0}

	overlaps := computeOverlaps(entriesMap, fileIndexMap)
	if len(overlaps) != 1 {
		t.Fatalf("expected 1 overlap pair, got %d", len(overlaps))
	}
	pair := overlaps[0]
	if pair.TestA != "TestA" || pair.TestB != "TestB" {
		t.Errorf("pair: %s <-> %s", pair.TestA, pair.TestB)
	}
	// TestA sigs: (0:1:1:3:2:1), (0:5:1:7:2:1)
	// TestB sigs: (0:1:1:3:2:1), (0:5:1:7:2:0)
	// Common: (0:1:1:3:2:1) = 1
	// Total: 3 unique sigs
	if pair.Common != 1 {
		t.Errorf("expected common=1, got %d", pair.Common)
	}
	if pair.Total != 3 {
		t.Errorf("expected total=3, got %d", pair.Total)
	}
}
