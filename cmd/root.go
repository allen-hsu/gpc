// Package cmd holds the cobra command tree for gpc.
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/allen-hsu/gpc/internal/play"
)

// Version is overridden at build time via -ldflags "-X .../cmd.Version=...".
var Version = "dev"

var (
	flagSA      string
	flagPackage string
	flagJSON    bool
	flagConfirm bool
)

var rootCmd = &cobra.Command{
	Use:   "gpc",
	Short: "Google Play Console CLI — a thin, agent-friendly wrapper over the Android Publisher API v3",
	Long: `gpc is to Google Play what asc is to App Store Connect.

It covers what the API can do: store listing text, images, AAB upload, track
releases, the data safety form and contact details. Everything else is
Console-only and must be clicked by a human — run 'gpc console-only' for the list.

Auth is a service-account JSON, resolved in this order:
  --service-account flag → $GPC_SERVICE_ACCOUNT → ~/.config/gpc/service-account.json

Output is a table on a terminal and JSON when piped (or with --json).
Google's own 4xx messages are printed verbatim — they are precise and are the
main feedback loop when iterating on a payload.

Hard facts baked into the error hints:
  1. The API cannot create an app. Create it in Play Console first; the package
     name is bound by the first AAB uploaded.
  2. A draft (never-published) app accepts a completed release only on the internal
     track; production/alpha/beta take --status draft until the first review passes.
  3. An open Play Console tab with an unsaved form steals the edit
     ("A change was made to the application outside of this Edit") — gpc
     reopens the edit and retries once.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and prints a single error line on failure.
func Execute() error {
	rootCmd.Version = Version
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpc:", err)
	}
	return err
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagSA, "service-account", "", "path to service-account JSON (default: $GPC_SERVICE_ACCOUNT, then ~/.config/gpc/service-account.json)")
	rootCmd.PersistentFlags().StringVarP(&flagPackage, "package", "p", os.Getenv("GPC_PACKAGE"), "Android package name, e.g. com.example.app (default: $GPC_PACKAGE)")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "force JSON output even on a terminal")
	rootCmd.PersistentFlags().BoolVar(&flagConfirm, "confirm", false, "required for destructive or user-visible operations (deleting images, non-draft releases)")
	rootCmd.AddCommand(consoleOnlyCmd)
}

// newClient resolves credentials and builds the API client. Commands that need
// a package call requirePackage first.
func newClient(ctx context.Context) (*play.Client, error) {
	sa, err := play.ResolveServiceAccount(flagSA)
	if err != nil {
		return nil, err
	}
	return play.New(ctx, sa, flagPackage)
}

func requirePackage() error {
	if flagPackage == "" {
		return fmt.Errorf("--package is required (or set GPC_PACKAGE)")
	}
	return nil
}

// requireConfirm gates operations that delete data or put a release in front
// of users — the asc convention.
func requireConfirm(what string) error {
	if !flagConfirm {
		return fmt.Errorf("%s needs --confirm", what)
	}
	return nil
}

func emit(v any, header []string, rows [][]string) error {
	return play.Emit(os.Stdout, flagJSON, v, header, rows)
}

var consoleOnlyCmd = &cobra.Command{
	Use:   "console-only",
	Short: "List what the Publisher API cannot do (a human must click these in Play Console)",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(`These have no API. A human must do them in Play Console (https://play.google.com/console):

  1. Create the app              All apps → Create app. Package name is bound by the first AAB.
  2. Content rating (IARC)       Policy → App content → Content ratings questionnaire.
  3. Target audience and content Policy → App content → Target audience.
  4. Health apps declaration     Policy → App content → Health apps. Non-medical trackers pick
                                 "Other" with a ≤150-character description; do not tick medical.
  5. App category                Grow → Store presence → Main store listing → App category.
  6. Countries / regions         Release → Production (or track) → Countries / regions.
  7. Submit for review           Publishing overview → "Send changes for review".
  8. Ads declaration, News apps, Government apps, Financial features, COVID-19 — under App content.

What gpc does cover: listing text (gpc listing), images (gpc images), AAB upload
(gpc bundle), releases (gpc track), data safety (gpc datasafety), contact details (gpc details).
`)
	},
}
