package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"sort"

	"github.com/goccy/tobari"
)

func main() {
	reader := tobari.ReadCoverArchivedFile()
	if reader == nil {
		fmt.Println("NO_SOURCES")
		os.Exit(0)
	}

	gr, err := gzip.NewReader(reader)
	if err != nil {
		log.Fatalf("gzip: %v", err)
	}
	defer gr.Close()

	type entry struct {
		name string
		hash string
	}
	var entries []entry
	tr := tar.NewReader(gr)
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
		h := sha256.Sum256(data)
		entries = append(entries, entry{
			name: hdr.Name,
			hash: fmt.Sprintf("%x", h),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	for _, e := range entries {
		fmt.Printf("%s\t%s\n", e.name, e.hash)
	}
}
