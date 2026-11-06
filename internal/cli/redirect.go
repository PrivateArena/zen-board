package cli

import (
	"log"
	"os"
)

// RedirectForJSON moves process-wide stdout onto stderr for the duration of one
// command, and returns the undo.
//
// The pipeline predates this package and prints progress with fmt.Printf in about
// thirty places. Threading an io.Writer through every layer to fix that is a large
// mechanical change with no functional payoff; under -json, stdout must carry
// exactly one JSON document, so the noise is moved to stderr instead. Because the
// swap is undone on return, two in-process invocations each produce exactly one
// document - which is why this returns a closure instead of doing it once at
// startup.
func RedirectForJSON() (restore func()) {
	savedOut := os.Stdout
	savedLog := log.Writer()
	os.Stdout = os.Stderr
	log.SetOutput(os.Stderr)
	var once bool
	return func() {
		if once {
			return
		}
		once = true
		os.Stdout = savedOut
		log.SetOutput(savedLog)
	}
}
