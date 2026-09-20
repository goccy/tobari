package tool

import (
	"context"
	"fmt"
	"os"

	"github.com/goccy/tobari/internal/telemetry"
	"github.com/goccy/tobari/internal/version"
)

const (
	// packagesDriverToolName is the tool name reported for a
	// GOPACKAGESDRIVER invocation, which runs no Go tool.
	packagesDriverToolName = "packagesdriver"

	// envToolexecImportPath is set by the go command for every toolexec
	// invocation to the import path of the package being built.
	envToolexecImportPath = "TOOLEXEC_IMPORTPATH"
)

// startToolTelemetry opens the span covering one tobari invocation and returns
// the function that closes it and writes the telemetry out. Everything is a
// no-op unless telemetry is enabled; see the telemetry package.
func startToolTelemetry(ctx context.Context, toolName string) (context.Context, func(error) error, error) {
	if os.Getenv(telemetry.EnvDir) == "" {
		return ctx, func(error) error { return nil }, nil
	}
	ver, err := version.Get()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get tobari version: %w", err)
	}
	rec, err := telemetry.FromEnv(
		telemetry.String("service.name", "tobari"),
		telemetry.String("service.version", ver.ID()),
		telemetry.Int("process.pid", os.Getpid()),
	)
	if err != nil {
		return nil, nil, err
	}
	ctx = telemetry.WithRecorder(ctx, rec)

	attrs := []telemetry.Attr{telemetry.String("tobari.tool", toolName)}
	if pkgPath := os.Getenv(envToolexecImportPath); pkgPath != "" {
		attrs = append(attrs, telemetry.String("tobari.package", pkgPath))
	}
	ctx, span := telemetry.Start(ctx, "tobari.tool", attrs...)

	// Processes started from here on — the real Go tool, but also the go
	// commands that in turn run tobari as toolexec or as packages driver —
	// inherit the environment, so this makes all of them continue this trace
	// under this invocation instead of starting one per go command.
	if err := os.Setenv(telemetry.EnvTraceparent, telemetry.Traceparent(ctx)); err != nil {
		return nil, nil, fmt.Errorf("failed to set %s: %w", telemetry.EnvTraceparent, err)
	}

	return ctx, func(err error) error {
		span.Fail(err)
		span.End()
		return rec.Flush()
	}, nil
}
