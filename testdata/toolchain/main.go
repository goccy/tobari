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

func coverageMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("X-GO-COVERAGE"); v != "" {
			tobari.Cover(func() { next.ServeHTTP(w, r) })
		} else {
			next.ServeHTTP(w, r)
		}
	})
}

func run(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/coverstart", func(w http.ResponseWriter, req *http.Request) {
		tobari.ClearCounters()
		fmt.Fprintf(w, "started")
	})

	mux.HandleFunc("/coverend", func(w http.ResponseWriter, req *http.Request) {
		tobari.WriteCoverProfile(tobari.SetMode, w)
	})

	mux.HandleFunc("/foo", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "foo")
	})

	srv := httptest.NewServer(coverageMiddleware(mux))
	defer srv.Close()

	cli := new(http.Client)

	// start coverage
	if err := func() error {
		req, err := http.NewRequest("GET", srv.URL+"/coverstart", nil)
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

	// access foo endpoint with coverage header
	if err := func() error {
		req, err := http.NewRequest("GET", srv.URL+"/foo", nil)
		if err != nil {
			return err
		}
		req.Header.Add("X-GO-COVERAGE", "true")
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

	// end coverage
	if err := func() error {
		req, err := http.NewRequest("GET", srv.URL+"/coverend", nil)
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
		return os.WriteFile("toolchain.cover", b, 0o600)
	}(); err != nil {
		return err
	}
	return nil
}

func add(a, b int) int {
	return a + b
}

func multiply(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	return a * b
}

func divide(a, b int) int {
	if b == 0 {
		panic("division by zero")
	}
	return a / b
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
