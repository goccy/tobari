package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/goccy/tobari"
)

func main() {
	reader, err := tobari.ReadCoverArchivedFile()
	if err != nil {
		log.Fatal(err)
	}
	if reader == nil {
		fmt.Println("NO_SOURCES")
		os.Exit(0)
	}

	gr, err := gzip.NewReader(reader)
	if err != nil {
		log.Fatalf("gzip: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var files []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("tar: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			log.Fatalf("read: %v", err)
		}
		content := string(data)
		// Verify content is non-empty and looks like Go source
		if len(content) == 0 {
			log.Fatalf("empty content for %s", hdr.Name)
		}
		if !strings.Contains(content, "package") {
			log.Fatalf("file %s does not look like Go source", hdr.Name)
		}
		files = append(files, hdr.Name)
	}

	sort.Strings(files)
	for _, f := range files {
		fmt.Println(f)
	}
}
