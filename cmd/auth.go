package cmd

import (
	"github.com/spf13/cobra"

	"github.com/allen-hsu/gpc/internal/play"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Credential checks",
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Verify the service account by opening and discarding an edit",
	Long: `Opens an edit on --package and deletes it without committing. Succeeds only
if the key is valid AND the service account has been invited to this app in
Play Console → Users and permissions.

Console page: Play Console → Users and permissions (to invite the account);
Google Cloud → IAM & Admin → Service accounts (to mint the JSON key).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		editID, err := c.WithEdit(cmd.Context(), false, func(string) error { return nil })
		if err != nil {
			return err
		}
		res := map[string]any{
			"ok":             true,
			"package":        c.Package,
			"serviceAccount": c.SAPath,
			"probeEditId":    editID,
			"committed":      false,
		}
		return emit(res, []string{"FIELD", "VALUE"}, [][]string{
			{"ok", "true"},
			{"package", c.Package},
			{"serviceAccount", c.SAPath},
			{"probeEditId", play.Truncate(editID, 40)},
		})
	},
}

func init() {
	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(authCmd)
}
