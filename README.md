# Tobari

[![DeepWiki](https://img.shields.io/badge/DeepWiki-goccy%2Ftobari-blue.svg?logo=data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACwAAAAyCAYAAAAnWDnqAAAAAXNSR0IArs4c6QAAA05JREFUaEPtmUtyEzEQhtWTQyQLHNak2AB7ZnyXZMEjXMGeK/AIi+QuHrMnbChYY7MIh8g01fJoopFb0uhhEqqcbWTp06/uv1saEDv4O3n3dV60RfP947Mm9/SQc0ICFQgzfc4CYZoTPAswgSJCCUJUnAAoRHOAUOcATwbmVLWdGoH//PB8mnKqScAhsD0kYP3j/Yt5LPQe2KvcXmGvRHcDnpxfL2zOYJ1mFwrryWTz0advv1Ut4CJgf5uhDuDj5eUcAUoahrdY/56ebRWeraTjMt/00Sh3UDtjgHtQNHwcRGOC98BJEAEymycmYcWwOprTgcB6VZ5JK5TAJ+fXGLBm3FDAmn6oPPjR4rKCAoJCal2eAiQp2x0vxTPB3ALO2CRkwmDy5WohzBDwSEFKRwPbknEggCPB/imwrycgxX2NzoMCHhPkDwqYMr9tRcP5qNrMZHkVnOjRMWwLCcr8ohBVb1OMjxLwGCvjTikrsBOiA6fNyCrm8V1rP93iVPpwaE+gO0SsWmPiXB+jikdf6SizrT5qKasx5j8ABbHpFTx+vFXp9EnYQmLx02h1QTTrl6eDqxLnGjporxl3NL3agEvXdT0WmEost648sQOYAeJS9Q7bfUVoMGnjo4AZdUMQku50McDcMWcBPvr0SzbTAFDfvJqwLzgxwATnCgnp4wDl6Aa+Ax283gghmj+vj7feE2KBBRMW3FzOpLOADl0Isb5587h/U4gGvkt5v60Z1VLG8BhYjbzRwyQZemwAd6cCR5/XFWLYZRIMpX39AR0tjaGGiGzLVyhse5C9RKC6ai42ppWPKiBagOvaYk8lO7DajerabOZP46Lby5wKjw1HCRx7p9sVMOWGzb/vA1hwiWc6jm3MvQDTogQkiqIhJV0nBQBTU+3okKCFDy9WwferkHjtxib7t3xIUQtHxnIwtx4mpg26/HfwVNVDb4oI9RHmx5WGelRVlrtiw43zboCLaxv46AZeB3IlTkwouebTr1y2NjSpHz68WNFjHvupy3q8TFn3Hos2IAk4Ju5dCo8B3wP7VPr/FGaKiG+T+v+TQqIrOqMTL1VdWV1DdmcbO8KXBz6esmYWYKPwDL5b5FA1a0hwapHiom0r/cKaoqr+27/XcrS5UwSMbQAAAABJRU5ErkJggg==)](https://deepwiki.com/goccy/tobari)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/goccy/tobari)](https://pkg.go.dev/github.com/goccy/tobari)
![Go](https://github.com/goccy/tobari/workflows/Go/badge.svg)

Tobari is a scoped coverage measurement tool for Go.

"Tobari" (帷) is a Japanese word meaning "curtain" or "veil," similar to the English word "cover".

Tobari provides coverage measurement capabilities that introduce a new concept called *Scoped Coverage* in addition to the coverage features provided by `runtime/coverage`.
This feature enables clear mapping between test code and its impact area, allowing for high-precision test code generation using AI and other tools.

# Background

To understand what Tobari enables, we first need to understand the current coverage mechanisms provided by Go.

The most common coverage measurement method we use is specifying coverage options with `go test`, such as `go test -cover`.
We can control coverage target packages with `-coverpkg` and output coverage results with `-coverprofile`.
However, using coverage through `go test` means we can only measure coverage when writing test code in Go.
This means coverage can only be measured for tests starting from functions like `func TestFoo(t *testing.T)`.
For example, it was not possible to measure coverage for a binary created with `go build` after the fact.

To improve this, Go version 1.20 and later allows the `-cover` option with `go build` and supports the `runtime/coverage` package.
With `go build -cover`, applications can be built with coverage instrumentation points.
The `runtime/coverage` API allows coverage counter initialization and result output at any timing.

This made it possible to measure coverage of servers implemented in Go using E2E testing tools not written in Go when implementing HTTP or gRPC servers.
Coverage functionality that was only available during `go test` execution became available at any timing during application runtime.

However, when actually trying to use `runtime/coverage`, you'll notice that some operational considerations are needed.
Coverage measurement with `runtime/coverage` increases coverage counters when processing passes through embedded coverage measurement points, but it doesn't care what caused the passage.
This is similar to traffic surveys for automobiles, where the number of cars passing a certain location is measured, but the type of cars is not considered.
This mechanism becomes problematic in situations like measuring coverage for server applications:

- To measure E2E test effectiveness, you want to measure coverage only when E2E testing tools access the server
  - Accesses other than from E2E tests should not be measured
- For test acceleration, you want to access the server concurrently from E2E testing tools, but manage access contexts separately
  - For example, when E2E test scenarios A and B exist, you want to measure accesses from A and B separately even when accessing the server concurrently
- Asynchronous processing by Goroutines created through methods not originating from E2E test requests should be excluded from coverage measurement

To meet these requirements, the server application must serialize and handle requests, ensuring that accesses from A and B are not processed simultaneously.
Additionally, you need to implement mechanisms to determine E2E test accesses by referencing headers and reject other requests.
Furthermore, what if you want to use this running server application for purposes other than E2E testing?
For example, when performing manual verification in parallel with E2E test verification. In this case, other requests may reach the server while E2E tests are running, and these requests should not be rejected. Also, serializing processing defeats the purpose of concurrent access for test acceleration.
Moreover, there's no way to ignore asynchronous processing not originating from E2E tests.

Therefore, I conceived the *Scoped Coverage* approach and decided to develop Tobari.

## What is *Scoped Coverage* ?

What *Scoped Coverage* provides over `runtime/coverage` coverage measurement is measuring "what passed through".
Using the E2E testing example, when measuring coverage from scenario A and B accesses, it distinguishes between "access from A" and "access from B".
Additionally, it measures only asynchronous processing originating from E2E tests. This enables coverage measurement limited to the scope you want to measure.
Concurrent access is also possible.

Coverage also needs to record "places that should be passed" in addition to recording "places that were passed".
Coverage is calculated using a formula like this:

```
Coverage (%) = (Places passed / Places that should be passed) * 100
```

In normal coverage measurement, all files in packages specified by `coverpkg` could be considered "places that should be passed", requiring no special processing.
However, Scoped Coverage is different. How should we define "places that should be passed" ?
For example, when calculating coverage for scenario A, if places that scenario A will never pass are included in "places that should be passed", coverage will never reach 100% no matter how hard you try.

Tobari determines "places that should be passed" by reverse calculation from the results of what was passed.
It extracts inter-function dependencies through static analysis in advance and calculates functions that could potentially be passed based on the dependency relationships of actually passed functions.
Considering this, the formula becomes:

```
Coverage (%) = (Places passed / All places in functions that could potentially be called from passed functions) * 100
```

## How to Use

Using tobari is very simple with 3 steps:

### 1. Installation

First, install the `tobari` tool with the following command:

```
go install github.com/goccy/tobari/cmd/tobari@latest
```

### 2. Use the API (like using the `runtime/coverage` package)

Using a gRPC server as an example:

`tobari.CoverWithName` serves as the entry point for coverage measurement.
Specify a name in the first argument to distinguish coverage units. The behavior with `runtime/coverage` is the same as specifying an empty name.
For gRPC, the name is obtained from metadata. If there's no metadata, it's treated as a normal request and the function exits without measuring coverage.

```go
tobariInterceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
  md, ok := metadata.FromIncomingContext(ctx)
  if !ok { return handler(ctx, req) }
  scenarioNames, exists := md["E2E-Tool-Scenario-Name"]
  if !exists { return handler(ctx, req) }
  if len(scenarioNames) == 0 { return handler(ctx, req) }

  scenarioName := scenarioNames[0]

  var (
    res any
    err error
  )
  tobari.CoverWithName(scenarioName, func() {
    res, err = handler(ctx, req)
  })
  return res, err
}

grpcServer := grpc.NewServer(grpc.UnaryInterceptor(tobariInterceptor))
```

When outputting coverage data, you can use `WriteCoverProfileByName` or `CoverProfileMap`:

- `WriteCoverProfileByName`: Get coverprofile results for a specified name
- `CoverProfileMap`: Return the relationship between names and coverprofiles in map format

These APIs can be executed by creating a separate gRPC server and calling specific endpoints when E2E tests finish.

### 3. Build the Application

Then, when building the application you want to measure coverage for, simply specify GOFLAGS as follows:

```console
GOFLAGS="$(tobari flags)" go build .
```

This example shows distinguishing coverage results by name, but you can also use it the same way as `runtime/coverage`.
For specific APIs, please [refer here](https://pkg.go.dev/github.com/goccy/tobari).

# Example

We will use a more practical example to give you a better idea of how to use tobari. The example code is located in examples/http, so you can run it on your own environment as well.

First, install the tobari CLI using the following command.

```
go install github.com/goccy/tobari/cmd/tobari@latest
```

Next, to run the example, clone the repository and move to the repository root.

```
git clone https://github.com/goccy/tobari.git
cd tobari
```

The examples/http directory contains code structured as follows.

```go
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
			// When measuring coverage, wrap the function with tobari.Cover.
			tobari.Cover(func() { next.ServeHTTP(w, r) })
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
		tobari.WriteCoverProfile(tobari.SetMode, w)
	})

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

	// end coverage.
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
		return os.WriteFile("test.cover", b, 0o600)
	}(); err != nil {
		return err
	}
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
```

Run this code using the following command.

```
GOFLAGS="$(tobari flags)" go run ./examples/http/main.go
```

Then, a `test.cover` file should be created in the current directory. Let’s view it using `go tool cover -html`.

```
go tool cover -html test.cover
```

This will produce an output like the following. Only the coverage related to foo is displayed.

<img width="880" height="505" alt="Image" src="https://github.com/user-attachments/assets/6738ffdc-5119-4719-96b8-e5fbd8d59d44" />
<img width="803" height="200" alt="Image" src="https://github.com/user-attachments/assets/f9b8fdf4-7d66-4e18-b9a0-af12dc06ff44" />
<img width="418" height="223" alt="Image" src="https://github.com/user-attachments/assets/ca52ac83-10aa-47a0-99b4-fb72fe1d863a" />

## How It Works

Tobari records which Goroutine increased the counter by obtaining the Goroutine ID (GID) and Parent Goroutine ID (PGID) when increasing coverage counters.
At the same time, when calling the coverage measurement entry function passed to `tobari.Cover` or `tobari.CoverWithName`, it creates a Goroutine and records its ID.
By tracing Goroutines that have the GID from when coverage measurement started as their parent, it can target only Goroutines related to coverage.
To implement this functionality, Tobari passes three options during `go build`: `-cover`, `-overlay`, and `-toolexec`.

- `-cover`: Added to have the Go compiler determine coverage instrumentation targets
- `-overlay`: Used to dynamically add APIs to the runtime package for obtaining GID and PGID, which are not public APIs
- `-toolexec`: Hooks execution of `go tool cover` and similar tools to embed measurement points that include GID and PGID

These options are output by the `tobari flags` command, so they can be added to `go build` options by simply specifying `GOFLAGS=$(tobari flags)`.

# License

MIT
