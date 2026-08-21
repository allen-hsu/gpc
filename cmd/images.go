package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/api/googleapi"

	"github.com/allen-hsu/gpc/internal/play"
)

var imageTypes = []string{
	"icon", "featureGraphic", "phoneScreenshots", "sevenInchScreenshots",
	"tenInchScreenshots", "tvBanner", "tvScreenshots", "wearScreenshots",
}

var (
	imgType    string
	imgLocales []string
	imgPath    string
	imgReplace bool
	imgDryRun  bool
)

var imagesCmd = &cobra.Command{
	Use:   "images",
	Short: "Store listing graphics (icon, feature graphic, screenshots) per locale",
	Long: `Console page: Grow → Store presence → Main store listing → Graphics.

--path may be a single file or a directory. A directory is uploaded in sorted
filename order (01.png, 02.png …); only .png/.jpg/.jpeg are picked up.

Layout conventions (either works):
  --path ./shots/              same files for every --locale
  --path ./shots/{locale}/     per-locale subfolder; "{locale}" is substituted

Sizes Play enforces (it returns a clear 400 otherwise):
  icon             512×512 PNG, 32-bit, ≤ 1 MB
  featureGraphic   1024×500 PNG/JPEG
  phoneScreenshots 2–8 images, 16:9 or 9:16, each side 320–3840 px

--replace deletes the existing images of that type/locale first (deleteall),
otherwise uploads are appended — screenshots accumulate up to the limit of 8.`,
}

var imagesUploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "Upload images of --type for each --locale",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		if !contains(imageTypes, imgType) {
			return fmt.Errorf("--type must be one of %s", strings.Join(imageTypes, ", "))
		}
		if len(imgLocales) == 0 {
			return fmt.Errorf("--locale is required (comma-separated Play locale codes, e.g. zh-TW,en-US,ja-JP)")
		}
		if imgReplace {
			if err := requireConfirm("--replace (deletes existing " + imgType + " images)"); err != nil {
				return err
			}
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		type up struct {
			Locale string `json:"locale"`
			File   string `json:"file"`
			ID     string `json:"id"`
			SHA1   string `json:"sha1"`
		}
		var done []up
		editID, err := c.WithEdit(cmd.Context(), !imgDryRun, func(editID string) error {
			for _, loc := range imgLocales {
				files, err := collectImages(strings.ReplaceAll(imgPath, "{locale}", loc))
				if err != nil {
					return fmt.Errorf("%s: %w", loc, err)
				}
				if imgReplace {
					if _, err := c.Svc.Edits.Images.Deleteall(c.Package, editID, loc, imgType).Context(cmd.Context()).Do(); err != nil {
						return play.Wrap(fmt.Sprintf("deleteall %s %s", imgType, loc), err)
					}
				}
				for _, f := range files {
					fh, err := os.Open(f)
					if err != nil {
						return err
					}
					res, err := c.Svc.Edits.Images.Upload(c.Package, editID, loc, imgType).
						Media(fh, googleapi.ContentType(mimeFor(f))).Context(cmd.Context()).Do()
					fh.Close()
					if err != nil {
						return play.Wrap(fmt.Sprintf("upload %s %s %s", imgType, loc, filepath.Base(f)), err)
					}
					u := up{Locale: loc, File: f}
					if res.Image != nil {
						u.ID, u.SHA1 = res.Image.Id, res.Image.Sha1
					}
					done = append(done, u)
					fmt.Fprintf(os.Stderr, "  uploaded %s %s %s\n", imgType, loc, filepath.Base(f))
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		rows := make([][]string, 0, len(done))
		for _, d := range done {
			rows = append(rows, []string{d.Locale, filepath.Base(d.File), play.Truncate(d.ID, 24)})
		}
		return emit(map[string]any{"editId": editID, "committed": !imgDryRun, "type": imgType, "uploaded": done},
			[]string{"LOCALE", "FILE", "IMAGE ID"}, rows)
	},
}

var imagesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List images of --type for each --locale",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		if !contains(imageTypes, imgType) {
			return fmt.Errorf("--type must be one of %s", strings.Join(imageTypes, ", "))
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		out := map[string]any{}
		rows := [][]string{}
		_, err = c.WithEdit(cmd.Context(), false, func(editID string) error {
			for _, loc := range imgLocales {
				res, err := c.Svc.Edits.Images.List(c.Package, editID, loc, imgType).Context(cmd.Context()).Do()
				if err != nil {
					return play.Wrap("list images "+loc, err)
				}
				out[loc] = res.Images
				for _, im := range res.Images {
					rows = append(rows, []string{loc, im.Id, im.Url})
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		return emit(out, []string{"LOCALE", "ID", "URL"}, rows)
	},
}

func collectImages(p string) ([]string, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return []string{p}, nil
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".png", ".jpg", ".jpeg":
			out = append(out, filepath.Join(p, e.Name()))
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no .png/.jpg files in %s", p)
	}
	return out, nil
}

func mimeFor(f string) string {
	switch strings.ToLower(filepath.Ext(f)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return "image/png"
	}
}

func init() {
	imagesCmd.PersistentFlags().StringVar(&imgType, "type", "", "image type: "+strings.Join(imageTypes, "|"))
	imagesCmd.PersistentFlags().StringSliceVar(&imgLocales, "locale", nil, "Play locale codes, comma-separated")
	imagesUploadCmd.Flags().StringVar(&imgPath, "path", "", "file or directory ({locale} is substituted per locale)")
	imagesUploadCmd.Flags().BoolVar(&imgReplace, "replace", false, "delete existing images of this type/locale before uploading")
	imagesUploadCmd.Flags().BoolVar(&imgDryRun, "dry-run", false, "upload inside an edit, then discard instead of committing")
	_ = imagesUploadCmd.MarkFlagRequired("path")
	imagesCmd.AddCommand(imagesUploadCmd, imagesListCmd)
	rootCmd.AddCommand(imagesCmd)
}
