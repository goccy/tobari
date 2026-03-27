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
}

// Hash returns a short hash derived from the toolexec options that affect
// code generation (EmbedCode, BuildTags). Trimpath and Race are excluded
// because they are detected later from compiler args and Go already
// includes them in its own cache key.
func (o BuildOpts) Hash() string {
	s := fmt.Sprintf("build-tags=%s,embed-code=%v", o.BuildTags, o.EmbedCode)
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}
