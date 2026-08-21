package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/api/googleapi"

	"github.com/allen-hsu/gpc/internal/play"
)

// 4 MB chunks + the 600s client timeout is the combination that got a
// 100 MB+ AAB through on a flaky connection; the Go client resumes failed
// chunks on its own.
const bundleChunkSize = 4 << 20

var (
	bundleRetries int
	bundleTrack   string
	bundleStatus  string
	bundleNotes   string
	bundleDryRun  bool
)

var bundleCmd = &cobra.Command{
	Use:   "bundle",
	Short: "Android App Bundles (.aab)",
	Long: `Console page: Release → App bundle explorer (after upload) / Release → Testing → Internal testing.

Uploading alone puts the bundle in the library; it is not released anywhere
until a track references its versionCode (see 'gpc track set', or pass --track
here to do both in one edit).`,
}

var bundleUploadCmd = &cobra.Command{
	Use:   "upload <app.aab>",
	Short: "Resumable upload of an .aab (4 MB chunks, 600 s socket timeout, retried)",
	Long: `Uploads the bundle with the resumable protocol and commits the edit.
Optionally assigns the resulting versionCode to a track in the same edit
(--track internal --status completed). On a brand-new (draft) app only the
internal track accepts completed; production/alpha/beta require --status draft.

Notes:
  • The package name inside the AAB must match --package; on a freshly
    created app the first upload is what binds the package name.
  • A reused versionCode is rejected; bump it and rebuild.
  • Upload signing: the AAB must be signed with the upload key registered in
    Play App Signing (EAS credentials handle this; a mismatch is a clear 400).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		path := args[0]
		if !strings.HasSuffix(strings.ToLower(path), ".aab") {
			return fmt.Errorf("%s is not an .aab (Play requires App Bundles; build with `eas build -p android` or `./gradlew bundleRelease`)", path)
		}
		st, err := os.Stat(path)
		if err != nil {
			return err
		}
		if bundleTrack != "" && !contains(trackStatuses, bundleStatus) {
			return fmt.Errorf("--status must be one of %s", strings.Join(trackStatuses, "|"))
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		var versionCode int64
		var sha256 string
		var editID string
		start := time.Now()
		for attempt := 1; attempt <= bundleRetries; attempt++ {
			editID, err = c.WithEdit(cmd.Context(), !bundleDryRun, func(eid string) error {
				fh, err := os.Open(path)
				if err != nil {
					return err
				}
				defer fh.Close()
				fmt.Fprintf(os.Stderr, "uploading %s (%.1f MB) as %s, attempt %d/%d\n", filepath.Base(path), float64(st.Size())/1e6, c.Package, attempt, bundleRetries)
				res, err := c.Svc.Edits.Bundles.Upload(c.Package, eid).
					Media(fh, googleapi.ContentType("application/octet-stream"), googleapi.ChunkSize(bundleChunkSize)).
					ProgressUpdater(func(current, total int64) {
						if total > 0 {
							fmt.Fprintf(os.Stderr, "\r  %3d%% %d/%d", current*100/total, current, total)
						}
					}).
					Context(cmd.Context()).Do()
				fmt.Fprintln(os.Stderr)
				if err != nil {
					return play.Wrap("upload bundle", err)
				}
				versionCode, sha256 = res.VersionCode, res.Sha256
				if bundleTrack != "" {
					notes, err := loadReleaseNotes(bundleNotes)
					if err != nil {
						return err
					}
					if err := setTrack(cmd, c, eid, bundleTrack, bundleStatus, []int64{versionCode}, notes, ""); err != nil {
						return err
					}
				}
				return nil
			})
			if err == nil {
				break
			}
			// Only network-ish failures are worth retrying; a 4xx from Play is final.
			if strings.Contains(err.Error(), "HTTP 4") || attempt == bundleRetries {
				return err
			}
			fmt.Fprintf(os.Stderr, "gpc: %v\n  retrying in 5s…\n", err)
			time.Sleep(5 * time.Second)
		}
		res := map[string]any{
			"editId":      editID,
			"committed":   !bundleDryRun,
			"versionCode": versionCode,
			"sha256":      sha256,
			"bytes":       st.Size(),
			"seconds":     int(time.Since(start).Seconds()),
		}
		if bundleTrack != "" {
			res["track"], res["status"] = bundleTrack, bundleStatus
		}
		rows := [][]string{
			{"versionCode", fmt.Sprint(versionCode)},
			{"sha256", sha256},
			{"bytes", fmt.Sprint(st.Size())},
			{"seconds", fmt.Sprint(int(time.Since(start).Seconds()))},
		}
		if bundleTrack != "" {
			rows = append(rows, []string{"track", bundleTrack + " (" + bundleStatus + ")"})
		}
		return emit(res, []string{"FIELD", "VALUE"}, rows)
	},
}

var bundleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List uploaded bundles (versionCode, sha256)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		var rows [][]string
		var out any
		_, err = c.WithEdit(cmd.Context(), false, func(editID string) error {
			res, err := c.Svc.Edits.Bundles.List(c.Package, editID).Context(cmd.Context()).Do()
			if err != nil {
				return play.Wrap("list bundles", err)
			}
			out = res.Bundles
			for _, b := range res.Bundles {
				rows = append(rows, []string{fmt.Sprint(b.VersionCode), b.Sha256})
			}
			return nil
		})
		if err != nil {
			return err
		}
		return emit(out, []string{"VERSION CODE", "SHA256"}, rows)
	},
}

func init() {
	bundleUploadCmd.Flags().IntVar(&bundleRetries, "retries", 3, "attempts for the whole upload on network failure (4xx is never retried)")
	bundleUploadCmd.Flags().StringVar(&bundleTrack, "track", "", "also assign the uploaded versionCode to this track in the same edit (internal|alpha|beta|production)")
	bundleUploadCmd.Flags().StringVar(&bundleStatus, "status", "draft", "release status when --track is given: draft|completed|inProgress|halted")
	bundleUploadCmd.Flags().BoolVar(&bundleDryRun, "dry-run", false, "upload inside an edit to validate signing/versionCode, then discard instead of committing")
	bundleUploadCmd.Flags().StringVar(&bundleNotes, "notes-dir", "", "release notes directory (<locale>.txt) when --track is given")
	bundleCmd.AddCommand(bundleUploadCmd, bundleListCmd)
	rootCmd.AddCommand(bundleCmd)
}
