package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/goccy/tobari"
)

func coverageMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name := r.Header.Get("X-GO-COVERAGE"); name != "" {
			// When measuring coverage, wrap the function with tobari.Cover.
			tobari.CoverWithName(name, func() { next.ServeHTTP(w, r) })
		} else {
			next.ServeHTTP(w, r)
		}
	})
}

var ch = make(chan struct{})

func run(ctx context.Context) error {
	mux := http.NewServeMux()

	// This is the endpoint used to start measuring coverage.
	mux.HandleFunc("/coverstart", func(w http.ResponseWriter, req *http.Request) {
		// Similar to ClearCounters in runtime/coverage, this resets the currently active counters.
		// It is intended to be called at the start of coverage measurement.
		tobari.ClearCounters()

		// This process is used to ensure that goroutines not subject to measurement are correctly ignored.
		// Normally, when coverage measurement begins, all goroutines are included in the measurement.
		// However, in Tobari, it is possible to count only specific processes, so the coverage of this goroutine will be ignored.
		go func() {
			<-ch
		}()

		fmt.Fprintf(w, "started")
	})

	// This is the endpoint used to stop coverage measurement and retrieve the results.
	mux.HandleFunc("/coverend", func(w http.ResponseWriter, req *http.Request) {
		// Writes data in coverprofile format.
		// The resulting output can be directly used with `go tool cover`.
		name := req.Header.Get("X-GO-COVERAGE")
		tobari.WriteCoverprofileByName(name, tobari.CountMode, w)
	})

	mux.HandleFunc("/foo", func(w http.ResponseWriter, req *http.Request) {
		name := req.Header.Get("X-GO-COVERAGE")
		if strings.HasSuffix(name, "2") {
			fmt.Fprintf(w, "foo2")
		} else {
			fmt.Fprintf(w, "foo1")
		}
	})
	mux.HandleFunc("/bar", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "bar")
	})

	// A middleware is added to switch behavior based on the presence of the coverage flag.
	srv := httptest.NewServer(coverageMiddleware(mux))
	defer srv.Close()

	cli := new(http.Client)

	// start coverage.
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

	// access foo endpoint with X-GO-COVERAGE header.
	if err := func() error {
		req, err := http.NewRequest("GET", srv.URL+"/foo", nil)
		if err != nil {
			return err
		}
		req.Header.Add("X-GO-COVERAGE", "foo1")
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

	// access foo endpoint with X-GO-COVERAGE header.
	if err := func() error {
		req, err := http.NewRequest("GET", srv.URL+"/foo", nil)
		if err != nil {
			return err
		}
		req.Header.Add("X-GO-COVERAGE", "foo2")
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

	// access foo endpoint with X-GO-COVERAGE header.
	if err := func() error {
		req, err := http.NewRequest("GET", srv.URL+"/foo", nil)
		if err != nil {
			return err
		}
		req.Header.Add("X-GO-COVERAGE", "foo2")
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
		req.Header.Add("X-GO-COVERAGE", "bar")
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

	// end coverage.
	if err := func() error {
		req, err := http.NewRequest("GET", srv.URL+"/coverend", nil)
		if err != nil {
			return err
		}
		req.Header.Add("X-GO-COVERAGE", "foo1")
		resp, err := cli.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return os.WriteFile("covername-foo1.cover", b, 0o600)
	}(); err != nil {
		return err
	}
	// end coverage.
	if err := func() error {
		req, err := http.NewRequest("GET", srv.URL+"/coverend", nil)
		if err != nil {
			return err
		}
		req.Header.Add("X-GO-COVERAGE", "foo2")
		resp, err := cli.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return os.WriteFile("covername-foo2.cover", b, 0o600)
	}(); err != nil {
		return err
	}
	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
