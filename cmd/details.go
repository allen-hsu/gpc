package cmd

import (
	"github.com/spf13/cobra"
	"google.golang.org/api/androidpublisher/v3"

	"github.com/allen-hsu/gpc/internal/play"
)

var (
	detEmail, detWebsite, detPhone, detDefaultLang string
)

var detailsCmd = &cobra.Command{
	Use:   "details",
	Short: "App-level contact details and default language",
	Long: `Console page: Grow → Store presence → Store settings → Store listing contact details.

Contact email is mandatory before a release can be reviewed; website is what
the "Developer" link on the store page points to (put the privacy policy and
support page there — Play's privacy-policy URL itself is under App content
and is Console-only).`,
}

var detailsSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set contact email / website / phone / default language (omitted flags keep current values)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		var final *androidpublisher.AppDetails
		editID, err := c.WithEdit(cmd.Context(), true, func(eid string) error {
			cur, err := c.Svc.Edits.Details.Get(c.Package, eid).Context(cmd.Context()).Do()
			if err != nil {
				return play.Wrap("get details", err)
			}
			if cmd.Flags().Changed("email") {
				cur.ContactEmail = detEmail
			}
			if cmd.Flags().Changed("website") {
				cur.ContactWebsite = detWebsite
			}
			if cmd.Flags().Changed("phone") {
				cur.ContactPhone = detPhone
			}
			if cmd.Flags().Changed("default-language") {
				cur.DefaultLanguage = detDefaultLang
			}
			final, err = c.Svc.Edits.Details.Update(c.Package, eid, cur).Context(cmd.Context()).Do()
			if err != nil {
				return play.Wrap("update details", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		return emitDetails(editID, final)
	},
}

var detailsGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show current contact details",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		var d *androidpublisher.AppDetails
		editID, err := c.WithEdit(cmd.Context(), false, func(eid string) error {
			var err error
			d, err = c.Svc.Edits.Details.Get(c.Package, eid).Context(cmd.Context()).Do()
			return play.Wrap("get details", err)
		})
		if err != nil {
			return err
		}
		return emitDetails(editID, d)
	},
}

func emitDetails(editID string, d *androidpublisher.AppDetails) error {
	return emit(map[string]any{"editId": editID, "details": d}, []string{"FIELD", "VALUE"}, [][]string{
		{"contactEmail", d.ContactEmail},
		{"contactWebsite", d.ContactWebsite},
		{"contactPhone", d.ContactPhone},
		{"defaultLanguage", d.DefaultLanguage},
	})
}

func init() {
	detailsSetCmd.Flags().StringVar(&detEmail, "email", "", "contact email (required by Play before review)")
	detailsSetCmd.Flags().StringVar(&detWebsite, "website", "", "contact website URL")
	detailsSetCmd.Flags().StringVar(&detPhone, "phone", "", "contact phone")
	detailsSetCmd.Flags().StringVar(&detDefaultLang, "default-language", "", "default listing language, e.g. zh-TW")
	detailsCmd.AddCommand(detailsSetCmd, detailsGetCmd)
	rootCmd.AddCommand(detailsCmd)
}
