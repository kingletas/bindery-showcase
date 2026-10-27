// Package version holds the one string that names this build. It is the
// placeholder the first pieces of the repository stand on; the guards, the
// commit gate and the message bus arrive in packages of their own.
package version

// Version is the release the code claims to be. It reads "dev" until a tag is cut.
const Version = "dev"

// String returns the name a command prints when it is asked which build it is.
func String() string {
	return "bindery " + Version
}
