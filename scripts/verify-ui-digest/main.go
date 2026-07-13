// Command verify-ui-digest prints the canonical buildinfo.Compute sha256
// digest for a UI dist directory, so CI can compare a booted binary's
// reported ui_digest (GET /api/v1/version) against a digest computed
// independently over the files it was staged from — proving the embed
// matches the staged build rather than the committed placeholder.
package main

import (
	"fmt"
	"os"

	"github.com/jaimegago/joe/internal/buildinfo"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: verify-ui-digest <dist-dir>")
		os.Exit(2)
	}
	digest, err := buildinfo.Compute(os.DirFS(os.Args[1]))
	if err != nil {
		fmt.Fprintln(os.Stderr, "compute:", err)
		os.Exit(1)
	}
	fmt.Println(digest)
}
