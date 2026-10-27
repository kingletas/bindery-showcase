// Package version holds the one string that names this build. The guards, the
// commit gate and the message bus each print it for --version.
package version

// Version is the release the code claims to be. It reads "dev" until a tag is cut.
const Version = "dev"

// String returns the name a command prints when it is asked which build it is.
func String() string {
	return "bindery " + Version
}
