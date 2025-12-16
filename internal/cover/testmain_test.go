package cover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertTestMain(t *testing.T) {
	// Create temporary directory for test outputs
	tempDir, err := os.MkdirTemp("", "testmain_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Copy input file to temp dir so we can modify it
	inputPath := filepath.Join("testdata", "testmain.go")
	tempInputPath := filepath.Join(tempDir, "testmain.go")
	inputContent, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("failed to read input file: %v", err)
	}
	if err := os.WriteFile(tempInputPath, inputContent, 0644); err != nil {
		t.Fatalf("failed to copy input file: %v", err)
	}

	expectedPath := filepath.Join("testdata", "converted_testmain.go")

	// Run the conversion (modifies the file in place)
	err = convertTestMain(tempInputPath)
	if err != nil {
		t.Fatalf("convertTestMain failed: %v", err)
	}

	// Read the actual output
	actualContent, err := os.ReadFile(tempInputPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	// Read the expected output
	expectedContent, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("failed to read expected file: %v", err)
	}

	// Compare the contents
	actualStr := normalizeGoCode(string(actualContent))
	expectedStr := normalizeGoCode(string(expectedContent))

	if actualStr != expectedStr {
		t.Errorf("Output does not match expected.\nActual:\n%s\n\nExpected:\n%s", actualStr, expectedStr)
		
		// Write actual output for debugging
		debugPath := filepath.Join(tempDir, "debug_actual.go")
		os.WriteFile(debugPath, actualContent, 0644)
		t.Logf("Actual output written to: %s", debugPath)
	}
}

func TestConvertTestMainMultiple(t *testing.T) {
	// Test with multiple test functions with different names
	tempDir, err := os.MkdirTemp("", "testmain_multiple_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Copy input file to temp dir so we can modify it
	inputPath := filepath.Join("testdata", "testmain_multiple.go")
	tempInputPath := filepath.Join(tempDir, "testmain_multiple.go")
	inputContent, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("failed to read input file: %v", err)
	}
	if err := os.WriteFile(tempInputPath, inputContent, 0644); err != nil {
		t.Fatalf("failed to copy input file: %v", err)
	}

	// Run the conversion (modifies the file in place)
	err = convertTestMain(tempInputPath)
	if err != nil {
		t.Fatalf("convertTestMain failed: %v", err)
	}

	// Read the actual output
	actualContent, err := os.ReadFile(tempInputPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	actualStr := string(actualContent)
	
	// Check that all test names are properly wrapped
	expectedPatterns := []string{
		`{"TestFoo", func(t *testing.T) { CoverWithName("TestFoo", func() { _xtest.TestFoo(t) }) }}`,
		`{"TestBar", func(t *testing.T) { CoverWithName("TestBar", func() { _xtest.TestBar(t) }) }}`,
		`{"TestBaz", func(t *testing.T) { CoverWithName("TestBaz", func() { _xtest.TestBaz(t) }) }}`,
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(actualStr, pattern) {
			t.Errorf("Expected pattern not found in output: %s", pattern)
		}
	}

	// Check that linkname declarations are present
	expectedLinknames := []string{
		"//go:linkname CoverWithName github.com/goccy/tobari.CoverWithName",
		"//go:linkname CoverProfileMap github.com/goccy/tobari.CoverProfileMap",
	}

	for _, linkname := range expectedLinknames {
		if !strings.Contains(actualStr, linkname) {
			t.Errorf("Expected linkname not found in output: %s", linkname)
		}
	}

	// Write actual output for debugging
	debugPath := filepath.Join(tempDir, "debug_multiple_actual.go")
	os.WriteFile(debugPath, actualContent, 0644)
	t.Logf("Multiple test output written to: %s", debugPath)
}

// normalizeGoCode normalizes Go code for comparison by removing extra whitespace
func normalizeGoCode(code string) string {
	lines := strings.Split(code, "\n")
	var normalized []string
	
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	
	return strings.Join(normalized, "\n")
}