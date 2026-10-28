// gh keeps the familiar CLI while rejecting unsupported argv before dispatch.
package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/wafflestudio/dicoco/internal/ghguard"
)

func main() {
	args, err := ghguard.Validate(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	const realGH = "/usr/local/libexec/dicoco-gh"
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GH_HOST", "GH_PAGER", "PAGER", "GH_PROMPT_DISABLED", "GH_DEBUG", "GH_FORCE_TTY":
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "GH_HOST=github.com", "GH_PAGER=cat", "PAGER=cat", "GH_PROMPT_DISABLED=1")
	if err := syscall.Exec(realGH, append([]string{realGH}, args...), env); err != nil {
		fmt.Fprintln(os.Stderr, "could not start GitHub CLI")
		os.Exit(1)
	}
}
