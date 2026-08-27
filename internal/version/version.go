package version

// Version and Commit are replaced by the release build through -ldflags -X.
var (
	Version = "0.0.0-dev"
	Commit  = "unknown"
)
