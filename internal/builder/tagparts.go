package builder

import (
	"zen-board/internal/script"
)

// tagParts splits a tag body on the argument separator. It delegates to the
// script package's splitter - the quote-aware one - because the compiler and the
// linter must not disagree about where an argument ends: a naive Split breaks
// `[banner:"Act I: Finale":"..."]` into four parts in the compiler and two in the
// linter.
func tagParts(body string) []string {
	return script.SplitParts(body)
}
