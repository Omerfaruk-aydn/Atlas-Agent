// Package patch applies single-file unified diffs.
package patch

import "errors"

// ErrMalformed is wrapped by the error Apply returns for a diff it cannot parse.
var ErrMalformed = errors.New("patch: malformed diff")

// ErrConflict is wrapped by the error Apply returns when a hunk does not fit
// the text.
var ErrConflict = errors.New("patch: hunk does not apply")

// Apply applies diff to original and returns the patched text.
//
// Lines. original is a sequence of lines separated by "\n". A final "\n" ends
// the last line and does not start another one, and the result ends with "\n"
// exactly when original does. The exception is an empty original: the result
// then ends with "\n" if it has any line at all, and is "" otherwise. A result
// with no lines is always "".
//
// The diff. Lines before the first hunk header (file headers such as "--- a",
// "+++ b", "diff ...") are ignored. A hunk starts with a header
//
//	@@ -OLDSTART[,OLDCOUNT] +NEWSTART[,NEWCOUNT] @@
//
// that may be followed by any text. A missing count is 1. The hunk body lines
// follow: " " context, "-" deleted, "+" added. A body line that starts with
// "\" is ignored (it is a "no newline" marker). An empty body line counts as
// an empty context line, since some tools strip the trailing space. A hunk
// ends at the next header or at the end of the diff. The empty piece after a
// trailing "\n" at the very end of diff is not a body line. A diff with no
// hunks returns original unchanged.
//
// Validation. OLDCOUNT must equal the number of context plus deleted lines
// and NEWCOUNT the number of context plus added lines (NEWSTART is not
// used). A line starting with "@@" that is not a valid header, or a body line
// with any other first character, is an error wrapping ErrMalformed.
//
// Position. OLDSTART is the 1-based line where the hunk's old lines (context
// and deleted, in order) begin. When OLDCOUNT is 0 the hunk only inserts, and
// OLDSTART is the line it goes after (0 means before the first line). Old
// lines must match the text exactly. If they do not match at the stated
// position, try the positions at offsets -1, +1, -2, +2, ... from it and use
// the first that matches, so equally near candidates resolve to the earlier
// one. A hunk may never be placed before the end of the previous hunk's
// matched lines. If nothing fits, the error wraps ErrConflict. Hunks are
// positioned in the order they appear, each against the original text.
func Apply(original, diff string) (string, error) {
	return "", errors.New("patch: not implemented")
}
