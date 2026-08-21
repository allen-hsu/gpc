package cmd

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/api/androidpublisher/v3"

	"github.com/allen-hsu/gpc/internal/play"
)

// dataSafetyHeader is the exact header row the API accepts. Anything else is
// rejected with "Invalid header row". It matches the CSV that Play Console's
// Data safety → Export exports, which is also the easiest way to obtain a
// template for a non-trivial declaration.
var dataSafetyHeader = []string{
	"Question ID (machine readable)",
	"Response ID (machine readable)",
	"Response value",
	"Answer requirement",
	"Human-friendly question label",
}

const noCollectionCSV = `Question ID (machine readable),Response ID (machine readable),Response value,Answer requirement,Human-friendly question label
PSL_DATA_COLLECTION_COLLECTS_PERSONAL_DATA,,false,REQUIRED,Does your app collect or share any of the required user data types?
`

var dsNoCollection bool

var datasafetyCmd = &cobra.Command{
	Use:   "datasafety",
	Short: "Data safety form (the applications.dataSafety method)",
	Long: `Console page: Policy → App content → Data safety.

This is the one Publisher API method that lives outside the edit lifecycle
(applications.dataSafety) and the only thing the 'applications' resource can
do — it cannot create apps.

CSV format. The header row must be byte-exact:

  Question ID (machine readable),Response ID (machine readable),Response value,Answer requirement,Human-friendly question label

"Collects nothing" is a complete, valid submission in two lines:

  PSL_DATA_COLLECTION_COLLECTS_PERSONAL_DATA,,false,REQUIRED,Does your app collect or share any of the required user data types?

Use --no-collection to send exactly that. For anything richer, fill the form
once in Console, use its Export CSV, commit that file, and push it from here
(e.g. after adding AdMob you must re-declare Device or other IDs).

After a push the form still shows "changes to review" in Console; the human
"Send changes for review" step on Publishing overview is what submits it.`,
}

var datasafetyPushCmd = &cobra.Command{
	Use:   "push [labels.csv]",
	Short: "Upload a data safety CSV (or --no-collection for the two-line minimal form)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		var body string
		switch {
		case dsNoCollection && len(args) == 0:
			body = noCollectionCSV
		case !dsNoCollection && len(args) == 1:
			b, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			body = string(b)
			if err := checkHeader(body); err != nil {
				return err
			}
		default:
			return fmt.Errorf("pass exactly one of a CSV path or --no-collection")
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		req := &androidpublisher.SafetyLabelsUpdateRequest{SafetyLabels: body}
		if _, err := c.Svc.Applications.DataSafety(c.Package, req).Context(cmd.Context()).Do(); err != nil {
			return play.Wrap("dataSafety", err)
		}
		lines := strings.Count(strings.TrimSpace(body), "\n")
		return emit(map[string]any{"ok": true, "rows": lines, "noCollection": dsNoCollection},
			[]string{"FIELD", "VALUE"}, [][]string{{"ok", "true"}, {"rows", fmt.Sprint(lines)}})
	},
}

var datasafetyTemplateCmd = &cobra.Command{
	Use:   "template",
	Short: "Print the minimal 'collects nothing' CSV to stdout",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(noCollectionCSV)
	},
}

func checkHeader(body string) error {
	r := csv.NewReader(strings.NewReader(body))
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		return fmt.Errorf("read CSV header: %w", err)
	}
	if len(head) > 0 {
		head[0] = strings.TrimPrefix(head[0], "\ufeff")
	}
	if strings.Join(head, ",") != strings.Join(dataSafetyHeader, ",") {
		return fmt.Errorf("CSV header mismatch.\n  got:  %s\n  want: %s\n  (the API would answer \"Invalid header row\")", strings.Join(head, ","), strings.Join(dataSafetyHeader, ","))
	}
	return nil
}

func init() {
	datasafetyPushCmd.Flags().BoolVar(&dsNoCollection, "no-collection", false, "declare that the app collects and shares no user data")
	datasafetyCmd.AddCommand(datasafetyPushCmd, datasafetyTemplateCmd)
	rootCmd.AddCommand(datasafetyCmd)
}
