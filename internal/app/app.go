// Package app defines the immutable description of a discovered Dropserve app.
package app

// Kind identifies how an app is served.
type Kind string

const (
	// KindStatic serves files directly from the app path.
	KindStatic Kind = "static"
	// KindCommand runs a supervised loopback HTTP process.
	KindCommand Kind = "command"
	// KindPHP serves PHP scripts through an optional FastCGI runtime pack.
	KindPHP Kind = "php"
)

// App is the read-only result of scanning one app folder or loose HTML file.
type App struct {
	Slug             string
	Name             string
	Path             string
	Kind             Kind
	Index            string
	LooseFile        bool
	DirectoryListing bool
	FileCount        int64
	Command          []string
	Runtime          string
	Detection        string
	HealthPath       string
	PortEnv          string
	Environment      map[string]string
	BaseHref         string
	Autostart        bool
	Status           string
	Port             int
	PrefersOwnPort   bool
	Databases        []string
}
