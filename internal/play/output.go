package play

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"
)

// IsTTY reports whether stdout is a terminal. Piped output gets JSON, a
// terminal gets a table — the asc convention.
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// Emit prints v as indented JSON when piped (or when forceJSON), otherwise
// renders rows through table. rows may be nil for scalar results.
func Emit(w io.Writer, forceJSON bool, v any, header []string, rows [][]string) error {
	if forceJSON || !IsTTY() {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	if rows == nil {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	return tw.Flush()
}

// Truncate shortens s for table cells.
func Truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
