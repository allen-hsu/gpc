package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/api/androidpublisher/v3"

	"github.com/allen-hsu/gpc/internal/play"
)

var testersGroups []string

var testersCmd = &cobra.Command{
	Use:   "testers",
	Short: "Tester Google Groups on a testing track (internal / alpha / beta)",
	Long: `Console page: Release → Testing → Internal/Closed testing → Testers tab.

The API exposes only the Google Groups list (e.g. my-testers@googlegroups.com).
Individual email lists and the opt-in link are Console-only. Internal testing
is capped at 100 testers; closed testing has no cap but needs the 12-tester /
14-day rule before production for new personal accounts.`,
}

var testersGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show tester groups on --track",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		if trackName == "" {
			return fmt.Errorf("--track is required (internal|alpha|beta)")
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		var t *androidpublisher.Testers
		if _, err := c.WithEdit(cmd.Context(), false, func(eid string) error {
			var err error
			t, err = c.Svc.Edits.Testers.Get(c.Package, eid, trackName).Context(cmd.Context()).Do()
			return play.Wrap("get testers", err)
		}); err != nil {
			return err
		}
		rows := [][]string{}
		for _, g := range t.GoogleGroups {
			rows = append(rows, []string{trackName, g})
		}
		if len(rows) == 0 {
			rows = append(rows, []string{trackName, "(no Google Groups; testers are managed by email in Console)"})
		}
		return emit(map[string]any{"track": trackName, "googleGroups": t.GoogleGroups}, []string{"TRACK", "GOOGLE GROUP"}, rows)
	},
}

var testersSetCmd = &cobra.Command{
	Use:   "set --track internal --groups a@googlegroups.com,b@googlegroups.com",
	Short: "Replace the Google Groups list on --track — needs --confirm",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		if trackName == "" {
			return fmt.Errorf("--track is required (internal|alpha|beta)")
		}
		if err := requireConfirm("replacing tester groups on " + trackName); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		editID, err := c.WithEdit(cmd.Context(), true, func(eid string) error {
			_, err := c.Svc.Edits.Testers.Update(c.Package, eid, trackName, &androidpublisher.Testers{GoogleGroups: testersGroups}).Context(cmd.Context()).Do()
			return play.Wrap("update testers", err)
		})
		if err != nil {
			return err
		}
		return emit(map[string]any{"editId": editID, "track": trackName, "googleGroups": testersGroups},
			[]string{"TRACK", "GOOGLE GROUPS"}, [][]string{{trackName, strings.Join(testersGroups, ", ")}})
	},
}

func init() {
	testersCmd.PersistentFlags().StringVar(&trackName, "track", "", "track: internal|alpha|beta")
	testersSetCmd.Flags().StringSliceVar(&testersGroups, "groups", nil, "Google Group addresses, comma-separated (empty clears)")
	testersCmd.AddCommand(testersGetCmd, testersSetCmd)
	rootCmd.AddCommand(testersCmd)
}

var countriesCmd = &cobra.Command{
	Use:   "countries",
	Short: "Country / region availability of a track (read-only in the API)",
	Long: `Console page: Release → Production (or a testing track) → Countries / regions.

The API can only READ availability (edits.countryavailability.get). Choosing
countries is Console-only — this command exists so an agent can verify the
human did it before asking for review.`,
}

var countriesGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show countries targeted by --track (default production)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		if trackName == "" {
			trackName = "production"
		}
		if trackName == "internal" {
			return fmt.Errorf("track internal does not support country availability (Play answers HTTP 400); internal testing is worldwide by design")
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		var av *androidpublisher.TrackCountryAvailability
		if _, err := c.WithEdit(cmd.Context(), false, func(eid string) error {
			var err error
			av, err = c.Svc.Edits.Countryavailability.Get(c.Package, eid, trackName).Context(cmd.Context()).Do()
			return play.Wrap("get country availability", err)
		}); err != nil {
			return err
		}
		codes := make([]string, 0, len(av.Countries))
		for _, ctry := range av.Countries {
			codes = append(codes, ctry.CountryCode)
		}
		summary := fmt.Sprintf("%d countries", len(codes))
		if av.RestOfWorld {
			summary += " + rest of world"
		}
		if av.SyncWithProduction {
			summary += " (synced with production)"
		}
		if len(codes) == 0 && !av.RestOfWorld {
			summary = "NONE — a human must pick countries in Console before review"
		}
		return emit(map[string]any{"track": trackName, "countries": codes, "restOfWorld": av.RestOfWorld, "syncWithProduction": av.SyncWithProduction},
			[]string{"TRACK", "SUMMARY", "COUNTRIES"}, [][]string{{trackName, summary, play.Truncate(strings.Join(codes, ","), 80)}})
	},
}

func init() {
	countriesCmd.PersistentFlags().StringVar(&trackName, "track", "", "track (default production)")
	countriesCmd.AddCommand(countriesGetCmd)
	rootCmd.AddCommand(countriesCmd)
}
