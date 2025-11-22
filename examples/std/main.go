package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/goccy/tobari"
)

var ch = make(chan struct{})

func run(ctx context.Context) error {
	mux := http.NewServeMux()

	go func() {
		<-ch
	}()

	mux.HandleFunc("/foo", func(w http.ResponseWriter, req *http.Request) {
		if v := req.Header.Get("xxxx"); v != "" {
			uncoveredFunc()
		}
		fmt.Fprintf(w, "foo")
	})
	mux.HandleFunc("/bar", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "bar")
	})

	// A middleware is added to switch behavior based on the presence of the coverage flag.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cli := new(http.Client)

	// access foo endpoint with X-GO-COVERAGE header.
	if err := func() error {
		req, err := http.NewRequest("GET", srv.URL+"/foo", nil)
		if err != nil {
			return err
		}
		resp, err := cli.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}(); err != nil {
		return err
	}

	// access bar endpoint without coverage header.
	if err := func() error {
		req, err := http.NewRequest("GET", srv.URL+"/bar", nil)
		if err != nil {
			return err
		}
		resp, err := cli.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}(); err != nil {
		return err
	}

	f, err := os.Create("out.cover")
	if err != nil {
		return err
	}
	defer f.Close()

	tobari.WriteAllCoverProfile(tobari.SetMode, f)
	return nil
}

func uncoveredFunc() {
	uncoveredFunc2()
}

func uncoveredFunc2() {
	uncoveredFunc3()
}

func uncoveredFunc3() {
	fmt.Println("uncovered func3")
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
