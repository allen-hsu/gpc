package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"google.golang.org/api/androidpublisher/v3"

	"github.com/allen-hsu/gpc/internal/play"
)

// listingFile is one <locale>.json under --dir. Field names follow the API.
type listingFile struct {
	Title            string `json:"title"`
	ShortDescription string `json:"shortDescription"`
	FullDescription  string `json:"fullDescription"`
	Video            string `json:"video,omitempty"`
}

// Play's published limits (characters, not bytes — CJK counts 1 each).
const (
	maxTitle = 30
	maxShort = 80
	maxFull  = 4000
)

var (
	listingDir     string
	listingDryRun  bool
	listingLocales []string
)

var listingCmd = &cobra.Command{
	Use:   "listing",
	Short: "Store listing text (title / short / full description) per locale",
	Long: `Console page: Grow → Store presence → Main store listing.

Directory layout (one file per Play locale code, e.g. zh-TW.json, en-US.json, ja-JP.json):

  {
    "title": "…",                 ≤ 30 chars
    "shortDescription": "…",      ≤ 80 chars
    "fullDescription": "…",       ≤ 4000 chars, emoji allowed (unlike App Store)
    "video": "https://youtube…"   optional
  }

Locale codes are Play's (zh-TW, not zh-Hant). Limits are validated locally
before any edit is opened so you get a per-locale, per-field count instead of
a generic 400.`,
}

var listingPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Upload every <locale>.json in --dir as the store listing",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		files, err := loadListings(listingDir, listingLocales)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return fmt.Errorf("no <locale>.json files in %s", listingDir)
		}
		var problems []string
		for loc, l := range files {
			problems = append(problems, checkLen(loc, "title", l.Title, maxTitle)...)
			problems = append(problems, checkLen(loc, "shortDescription", l.ShortDescription, maxShort)...)
			problems = append(problems, checkLen(loc, "fullDescription", l.FullDescription, maxFull)...)
		}
		if len(problems) > 0 {
			return fmt.Errorf("listing limits exceeded:\n  %s", strings.Join(problems, "\n  "))
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		var pushed []string
		editID, err := c.WithEdit(cmd.Context(), !listingDryRun, func(editID string) error {
			for _, loc := range sortedKeys(files) {
				l := files[loc]
				body := &androidpublisher.Listing{
					Language:         loc,
					Title:            l.Title,
					ShortDescription: l.ShortDescription,
					FullDescription:  l.FullDescription,
					Video:            l.Video,
				}
				if _, err := c.Svc.Edits.Listings.Update(c.Package, editID, loc, body).Context(cmd.Context()).Do(); err != nil {
					return play.Wrap("listing "+loc, err)
				}
				pushed = append(pushed, loc)
			}
			return nil
		})
		if err != nil {
			return err
		}
		rows := make([][]string, 0, len(pushed))
		for _, loc := range pushed {
			l := files[loc]
			rows = append(rows, []string{loc, fmt.Sprint(utf8.RuneCountInString(l.Title)), fmt.Sprint(utf8.RuneCountInString(l.ShortDescription)), fmt.Sprint(utf8.RuneCountInString(l.FullDescription))})
		}
		return emit(map[string]any{"editId": editID, "committed": !listingDryRun, "locales": pushed},
			[]string{"LOCALE", "TITLE", "SHORT", "FULL"}, rows)
	},
}

var listingPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Download the current listing into --dir as <locale>.json",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		var got []*androidpublisher.Listing
		if _, err := c.WithEdit(cmd.Context(), false, func(editID string) error {
			res, err := c.Svc.Edits.Listings.List(c.Package, editID).Context(cmd.Context()).Do()
			if err != nil {
				return play.Wrap("list listings", err)
			}
			got = res.Listings
			return nil
		}); err != nil {
			return err
		}
		if err := os.MkdirAll(listingDir, 0o755); err != nil {
			return err
		}
		rows := [][]string{}
		out := map[string]listingFile{}
		for _, l := range got {
			if len(listingLocales) > 0 && !contains(listingLocales, l.Language) {
				continue
			}
			f := listingFile{Title: l.Title, ShortDescription: l.ShortDescription, FullDescription: l.FullDescription, Video: l.Video}
			b, _ := json.MarshalIndent(f, "", "  ")
			p := filepath.Join(listingDir, l.Language+".json")
			if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
				return err
			}
			out[l.Language] = f
			rows = append(rows, []string{l.Language, play.Truncate(l.Title, 30), p})
		}
		return emit(out, []string{"LOCALE", "TITLE", "FILE"}, rows)
	},
}

func loadListings(dir string, only []string) (map[string]listingFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]listingFile{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		loc := strings.TrimSuffix(e.Name(), ".json")
		if len(only) > 0 && !contains(only, loc) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var l listingFile
		if err := json.Unmarshal(b, &l); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out[loc] = l
	}
	return out, nil
}

func checkLen(loc, field, s string, max int) []string {
	if n := utf8.RuneCountInString(s); n > max {
		return []string{fmt.Sprintf("%s %s: %d chars (max %d)", loc, field, n, max)}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func init() {
	listingCmd.PersistentFlags().StringVar(&listingDir, "dir", "./play-metadata", "directory of <locale>.json files")
	listingCmd.PersistentFlags().StringSliceVar(&listingLocales, "locale", nil, "restrict to these locales (comma-separated)")
	listingPushCmd.Flags().BoolVar(&listingDryRun, "dry-run", false, "validate against the API inside an edit, then discard instead of committing")
	listingCmd.AddCommand(listingPushCmd, listingPullCmd)
	rootCmd.AddCommand(listingCmd)
}
