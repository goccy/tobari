package utils

// Environment variable names used for IPC between tobari processes.
const (
	// EnvPackagesDriver is set to "1" when tobari is invoked as a GOPACKAGESDRIVER.
	EnvPackagesDriver = "TOBARI_PACKAGES_DRIVER"

	// EnvGoListFile holds the path to a temp file containing go list -deps -json output.
	EnvGoListFile = "TOBARI_GO_LIST_FILE"

	// EnvCoverPkgPathsFile holds the path to a temp file containing cover target package paths.
	EnvCoverPkgPathsFile = "TOBARI_COVER_PKG_PATHS_FILE"
)
