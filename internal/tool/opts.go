package tool

// BuildOpts bundles build configuration that is threaded through
// compile, link, and inner build phases.
type BuildOpts struct {
	EmbedCode bool
	Trimpath  bool
	Race      bool
	BuildTags string
}
