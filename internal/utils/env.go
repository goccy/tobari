package utils

const (
	// CoverOutputDirName is the directory an instrumented program writes
	// tobari.json into; see the testdeps overlay.
	CoverOutputDirName = "tobari"

	// AnalysisReportDirName is the subdirectory of CoverOutputDirName holding
	// the analysis report of every analyzed target.
	AnalysisReportDirName = "analysis"
)

// Environment variable names read by tobari processes.
const (
	// EnvPackagesDriver is set to "1" when tobari is invoked as a GOPACKAGESDRIVER.
	EnvPackagesDriver = "TOBARI_PACKAGES_DRIVER"

	// EnvGoListFile holds the path to a temp file containing go list -deps -json output.
	EnvGoListFile = "TOBARI_GO_LIST_FILE"

	// EnvCoverDir names the directory under which an instrumented program
	// writes its coverage results, into a CoverOutputDirName subdirectory. The
	// build writes its analysis reports next to them.
	EnvCoverDir = "TOBARI_COVERDIR"

	// EnvCoverPkgPathsFile holds the path to a temp file containing cover target package paths.
	EnvCoverPkgPathsFile = "TOBARI_COVER_PKG_PATHS_FILE"
)
