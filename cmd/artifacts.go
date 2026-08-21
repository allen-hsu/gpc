package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/api/googleapi"

	"github.com/allen-hsu/gpc/internal/play"
)

// ---- deobfuscation (mapping.txt / native symbols) ----

var (
	mappingCode int64
	mappingKind string
)

var mappingCmd = &cobra.Command{
	Use:   "mapping",
	Short: "ProGuard/R8 mapping and native debug symbols for crash deobfuscation",
	Long: `Console page: Release → App bundle explorer → <version> → Downloads → "Re-upload mapping file".

EAS/Gradle release builds emit android/app/build/outputs/mapping/release/mapping.txt.
Upload it against the versionCode it belongs to so Play Console crashes and
'gpc vitals' show readable stack traces. Type "nativeCode" takes a zip of .so
symbol files (native-debug-symbols.zip).`,
}

var mappingUploadCmd = &cobra.Command{
	Use:   "upload <mapping.txt|native-debug-symbols.zip> --version-code N",
	Short: "Upload a deobfuscation file for one versionCode",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		if mappingCode == 0 {
			return fmt.Errorf("--version-code is required (see `gpc bundle list`)")
		}
		if mappingKind != "proguard" && mappingKind != "nativeCode" {
			return fmt.Errorf("--type must be proguard or nativeCode")
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		editID, err := c.WithEdit(cmd.Context(), true, func(eid string) error {
			fh, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer fh.Close()
			_, err = c.Svc.Edits.Deobfuscationfiles.Upload(c.Package, eid, mappingCode, mappingKind).
				Media(fh, googleapi.ContentType("application/octet-stream")).Context(cmd.Context()).Do()
			return play.Wrap("upload deobfuscation file", err)
		})
		if err != nil {
			return err
		}
		return emit(map[string]any{"editId": editID, "versionCode": mappingCode, "type": mappingKind, "file": args[0]},
			[]string{"FIELD", "VALUE"}, [][]string{{"versionCode", fmt.Sprint(mappingCode)}, {"type", mappingKind}, {"file", filepath.Base(args[0])}})
	},
}

// ---- internal app sharing ----

var sharingCmd = &cobra.Command{
	Use:   "sharing",
	Short: "Internal app sharing — upload a build and get an install link, no edit, no track",
	Long: `Console page: Release → Setup → Internal app sharing (to allow uploaders/testers).

The fastest way to hand a build to a tester: no edit, no release, no review.
Requires the app to have been published at least once (otherwise 400
NOT_PUBLISHED) — on a brand-new app use the internal track instead.
The link works for the email addresses allow-listed under Internal app sharing
and expires after 60 days. Debuggable builds and unsigned bundles are accepted,
so it is also the only way to distribute a debug build through Play.`,
}

var sharingUploadCmd = &cobra.Command{
	Use:   "upload <app.aab|app.apk>",
	Short: "Upload to internal app sharing and print the download URL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		fh, err := os.Open(args[0])
		if err != nil {
			return err
		}
		defer fh.Close()
		var url, sha, fp string
		ext := strings.ToLower(filepath.Ext(args[0]))
		switch ext {
		case ".aab":
			res, err := c.Svc.Internalappsharingartifacts.Uploadbundle(c.Package).
				Media(fh, googleapi.ContentType("application/octet-stream"), googleapi.ChunkSize(bundleChunkSize)).Context(cmd.Context()).Do()
			if err != nil {
				return play.Wrap("internal sharing upload", err)
			}
			url, sha, fp = res.DownloadUrl, res.Sha256, res.CertificateFingerprint
		case ".apk":
			res, err := c.Svc.Internalappsharingartifacts.Uploadapk(c.Package).
				Media(fh, googleapi.ContentType("application/vnd.android.package-archive"), googleapi.ChunkSize(bundleChunkSize)).Context(cmd.Context()).Do()
			if err != nil {
				return play.Wrap("internal sharing upload", err)
			}
			url, sha, fp = res.DownloadUrl, res.Sha256, res.CertificateFingerprint
		default:
			return fmt.Errorf("%s: expected .aab or .apk", args[0])
		}
		return emit(map[string]any{"downloadUrl": url, "sha256": sha, "certificateFingerprint": fp},
			[]string{"FIELD", "VALUE"}, [][]string{{"downloadUrl", url}, {"sha256", sha}, {"certificate", fp}})
	},
}

func init() {
	mappingUploadCmd.Flags().Int64Var(&mappingCode, "version-code", 0, "versionCode the file belongs to")
	mappingUploadCmd.Flags().StringVar(&mappingKind, "type", "proguard", "proguard|nativeCode")
	mappingCmd.AddCommand(mappingUploadCmd)
	sharingCmd.AddCommand(sharingUploadCmd)
	rootCmd.AddCommand(mappingCmd, sharingCmd)
}
