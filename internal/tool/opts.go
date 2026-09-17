package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// BuildOpts bundles build configuration that is threaded through
// compile, link, and inner build phases.
type BuildOpts struct {
	EmbedCode bool
	Trimpath  bool
	Race      bool
	BuildTags string
	// ExcludeAnalysis lists package-path prefixes to omit from the
	// whole-program dependency analysis. See cover.CreateMainDeps.
	ExcludeAnalysis []string
	// PassedBlocksOnly makes the binary record only the blocks that were
	// actually passed. The "places that should be passed" are then left for
	// the consumer of the results to derive, so the whole-program dependency
	// analysis that would compute them is not run. See cover.Options.
	PassedBlocksOnly bool
}

// Hash returns a short hash derived from the toolexec options that affect
// code generation (EmbedCode, BuildTags). Trimpath and Race are excluded
// because they are detected later from compiler args and Go already
// includes them in its own cache key.
//
// ExcludeAnalysis and PassedBlocksOnly are deliberately NOT hashed here. This hash is emitted for
// every tool's `-V=full` probe, so it feeds the actionID of every package
// compiled with that tool; hashing them here would invalidate the whole
// dependency closure even though only cover-instrumented packages can change.
// They are folded into the cover tool's identity alone instead; see
// handleVersionFull.
func (o BuildOpts) Hash() string {
	s := fmt.Sprintf("build-tags=%s,embed-code=%v", o.BuildTags, o.EmbedCode)
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}
