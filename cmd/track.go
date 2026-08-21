package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/api/androidpublisher/v3"

	"github.com/allen-hsu/gpc/internal/play"
)

var trackStatuses = []string{"draft", "completed", "inProgress", "halted"}

var (
	trackName     string
	trackStatus   string
	trackCodes    []string
	trackNotesDir string
	trackRelName  string
	trackFraction float64
	promoteFrom   string
	promoteTo     string
	promoteStatus string
)

var trackCmd = &cobra.Command{
	Use:   "track",
	Short: "Releases on a track (internal / alpha / beta / production)",
	Long: `Console page: Release → Testing → Internal/Closed/Open testing, or Release → Production.

A release = a list of versionCodes + status + per-locale notes. The status
state machine Play accepts:

  draft       saved, not rolled out. On a never-published (draft) app this is the
              only status production/alpha/beta accept ("Only releases with status
              draft may be created on draft app"); internal is exempt.
  completed   fully rolled out (100%).
  inProgress  staged rollout; requires --rollout 0<f<1.
  halted      a paused inProgress rollout.

Setting a track REPLACES its release list. gpc sends exactly one release.

Release notes: --notes-dir holds <locale>.txt files (zh-TW.txt, en-US.txt …),
each ≤ 500 characters.`,
}

var trackSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Put a release (version codes + status + notes) on a track",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		if !contains(trackStatuses, trackStatus) {
			return fmt.Errorf("--status must be one of %s", strings.Join(trackStatuses, "|"))
		}
		codes, err := parseCodes(trackCodes)
		if err != nil {
			return err
		}
		notes, err := loadReleaseNotes(trackNotesDir)
		if err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		editID, err := c.WithEdit(cmd.Context(), true, func(eid string) error {
			return setTrack(cmd, c, eid, trackName, trackStatus, codes, notes, trackRelName)
		})
		if err != nil {
			return err
		}
		return emit(map[string]any{"editId": editID, "track": trackName, "status": trackStatus, "versionCodes": codes, "noteLocales": noteLocales(notes)},
			[]string{"FIELD", "VALUE"}, [][]string{
				{"track", trackName},
				{"status", trackStatus},
				{"versionCodes", strings.Join(trackCodes, ",")},
				{"notes", strings.Join(noteLocales(notes), ",")},
			})
	},
}

var trackGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show the releases currently on --track (or all tracks)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		var tracks []*androidpublisher.Track
		_, err = c.WithEdit(cmd.Context(), false, func(eid string) error {
			if trackName != "" {
				t, err := c.Svc.Edits.Tracks.Get(c.Package, eid, trackName).Context(cmd.Context()).Do()
				if err != nil {
					return play.Wrap("get track "+trackName, err)
				}
				tracks = []*androidpublisher.Track{t}
				return nil
			}
			res, err := c.Svc.Edits.Tracks.List(c.Package, eid).Context(cmd.Context()).Do()
			if err != nil {
				return play.Wrap("list tracks", err)
			}
			tracks = res.Tracks
			return nil
		})
		if err != nil {
			return err
		}
		var rows [][]string
		for _, t := range tracks {
			if len(t.Releases) == 0 {
				rows = append(rows, []string{t.Track, "-", "-", "-"})
			}
			for _, r := range t.Releases {
				codes := make([]string, len(r.VersionCodes))
				for i, vc := range r.VersionCodes {
					codes[i] = strconv.FormatInt(vc, 10)
				}
				rows = append(rows, []string{t.Track, r.Status, strings.Join(codes, ","), r.Name})
			}
		}
		return emit(tracks, []string{"TRACK", "STATUS", "VERSION CODES", "NAME"}, rows)
	},
}

var trackPromoteCmd = &cobra.Command{
	Use:   "promote",
	Short: "Copy the newest release on --from to --to (notes included)",
	Long: `Reads the first release on --from and writes it to --to with --status
(default completed). On a draft app, promoting to production/alpha/beta needs
--status draft; internal accepts completed.

Console equivalent: Release → <track> → Promote release.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		if !contains(trackStatuses, promoteStatus) {
			return fmt.Errorf("--status must be one of %s", strings.Join(trackStatuses, "|"))
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		var src *androidpublisher.TrackRelease
		editID, err := c.WithEdit(cmd.Context(), true, func(eid string) error {
			t, err := c.Svc.Edits.Tracks.Get(c.Package, eid, promoteFrom).Context(cmd.Context()).Do()
			if err != nil {
				return play.Wrap("get track "+promoteFrom, err)
			}
			if len(t.Releases) == 0 {
				return fmt.Errorf("track %s has no releases to promote", promoteFrom)
			}
			src = t.Releases[0]
			return setTrack(cmd, c, eid, promoteTo, promoteStatus, src.VersionCodes, src.ReleaseNotes, src.Name)
		})
		if err != nil {
			return err
		}
		return emit(map[string]any{"editId": editID, "from": promoteFrom, "to": promoteTo, "status": promoteStatus, "versionCodes": src.VersionCodes},
			[]string{"FIELD", "VALUE"}, [][]string{
				{"from", promoteFrom}, {"to", promoteTo}, {"status", promoteStatus},
				{"versionCodes", fmt.Sprint(src.VersionCodes)},
			})
	},
}

// setTrack writes a single release onto a track inside an open edit.
func setTrack(cmd *cobra.Command, c *play.Client, editID, track, status string, codes []int64, notes []*androidpublisher.LocalizedText, name string) error {
	if status != "draft" {
		if err := requireConfirm(fmt.Sprintf("status %s on track %s (visible to testers/users)", status, track)); err != nil {
			return err
		}
	}
	rel := &androidpublisher.TrackRelease{
		Status:       status,
		VersionCodes: codes,
		ReleaseNotes: notes,
		Name:         name,
	}
	if status == "inProgress" {
		if trackFraction <= 0 || trackFraction >= 1 {
			return fmt.Errorf("--rollout must be between 0 and 1 (exclusive) for status inProgress")
		}
		rel.UserFraction = trackFraction
	}
	body := &androidpublisher.Track{Track: track, Releases: []*androidpublisher.TrackRelease{rel}}
	if _, err := c.Svc.Edits.Tracks.Update(c.Package, editID, track, body).Context(cmd.Context()).Do(); err != nil {
		return play.Wrap("set track "+track, err)
	}
	return nil
}

func parseCodes(xs []string) ([]int64, error) {
	if len(xs) == 0 {
		return nil, fmt.Errorf("--version-codes is required (comma-separated; see `gpc bundle list`)")
	}
	out := make([]int64, 0, len(xs))
	for _, s := range xs {
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("bad version code %q", s)
		}
		out = append(out, n)
	}
	return out, nil
}

// loadReleaseNotes reads <locale>.txt files; an empty dir name means no notes.
func loadReleaseNotes(dir string) ([]*androidpublisher.LocalizedText, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []*androidpublisher.LocalizedText
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		text := strings.TrimSpace(string(b))
		if n := len([]rune(text)); n > 500 {
			return nil, fmt.Errorf("%s: release notes are %d chars (max 500)", e.Name(), n)
		}
		out = append(out, &androidpublisher.LocalizedText{Language: strings.TrimSuffix(e.Name(), ".txt"), Text: text})
	}
	return out, nil
}

func noteLocales(notes []*androidpublisher.LocalizedText) []string {
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.Language)
	}
	return out
}

func init() {
	trackCmd.PersistentFlags().StringVar(&trackName, "track", "", "track name: internal|alpha|beta|production")
	trackSetCmd.Flags().StringVar(&trackStatus, "status", "draft", "release status: "+strings.Join(trackStatuses, "|"))
	trackSetCmd.Flags().StringSliceVar(&trackCodes, "version-codes", nil, "version codes to include, comma-separated")
	trackSetCmd.Flags().StringVar(&trackNotesDir, "notes-dir", "", "directory of <locale>.txt release notes")
	trackSetCmd.Flags().StringVar(&trackRelName, "name", "", "release name shown in Console (defaults to the version name)")
	trackSetCmd.Flags().Float64Var(&trackFraction, "rollout", 0, "user fraction for status inProgress, e.g. 0.1")
	_ = trackSetCmd.MarkFlagRequired("track")
	trackPromoteCmd.Flags().StringVar(&promoteFrom, "from", "internal", "source track")
	trackPromoteCmd.Flags().StringVar(&promoteTo, "to", "production", "destination track")
	trackPromoteCmd.Flags().StringVar(&promoteStatus, "status", "completed", "status on the destination: "+strings.Join(trackStatuses, "|"))
	trackPromoteCmd.Flags().Float64Var(&trackFraction, "rollout", 0, "user fraction for status inProgress")
	trackCmd.AddCommand(trackSetCmd, trackGetCmd, trackPromoteCmd)
	rootCmd.AddCommand(trackCmd)
}
