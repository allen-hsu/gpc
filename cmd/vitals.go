package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/api/playdeveloperreporting/v1beta1"

	"github.com/allen-hsu/gpc/internal/play"
)

var (
	vitalsDays int
	vitalsType string
	vitalsMax  int64
)

var vitalsCmd = &cobra.Command{
	Use:   "vitals",
	Short: "Crashes and ANRs from the Play Developer Reporting API",
	Long: `Console page: Quality → Android vitals → Crashes and ANRs.

This uses a second API (playdeveloperreporting.googleapis.com) that must be
enabled once in the Google Cloud project owning the service account; the
error hint tells you where. Data lags Console by a few hours and only exists
once real users (not internal testers on debug builds) have run the app.

Upload mapping.txt with 'gpc mapping upload' or the stack traces are obfuscated.`,
}

var vitalsIssuesCmd = &cobra.Command{
	Use:   "issues",
	Short: "Top crash/ANR clusters in the last --days, ordered by affected users",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		end := time.Now().UTC().Truncate(time.Hour)
		start := end.Add(-time.Duration(vitalsDays) * 24 * time.Hour)
		call := c.Reporting.Vitals.Errors.Issues.Search("apps/" + c.Package).
			IntervalStartTimeYear(int64(start.Year())).IntervalStartTimeMonth(int64(start.Month())).IntervalStartTimeDay(int64(start.Day())).IntervalStartTimeHours(int64(start.Hour())).IntervalStartTimeTimeZoneId("UTC").
			IntervalEndTimeYear(int64(end.Year())).IntervalEndTimeMonth(int64(end.Month())).IntervalEndTimeDay(int64(end.Day())).IntervalEndTimeHours(int64(end.Hour())).IntervalEndTimeTimeZoneId("UTC").
			OrderBy("distinctUsers desc").PageSize(vitalsMax)
		if vitalsType != "" {
			call = call.Filter("errorIssueType = " + strings.ToUpper(vitalsType))
		}
		res, err := call.Context(cmd.Context()).Do()
		if err != nil {
			return play.Wrap("search error issues", err)
		}
		rows := make([][]string, 0, len(res.ErrorIssues))
		for _, is := range res.ErrorIssues {
			ver := ""
			if is.LastAppVersion != nil {
				ver = fmt.Sprint(is.LastAppVersion.VersionCode)
			}
			rows = append(rows, []string{is.Type, fmt.Sprint(is.DistinctUsers), fmt.Sprint(is.ErrorReportCount), ver, play.Truncate(is.Cause, 40), play.Truncate(is.Location, 40)})
		}
		if len(rows) == 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "no error issues in the last %d days\n", vitalsDays)
		}
		return emit(res.ErrorIssues, []string{"TYPE", "USERS", "REPORTS", "LAST VC", "CAUSE", "LOCATION"}, rows)
	},
}

var vitalsReportCmd = &cobra.Command{
	Use:   "report <issueName>",
	Short: "Fetch sample stack traces for one issue (name from 'vitals issues' JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		end := time.Now().UTC().Truncate(time.Hour)
		start := end.Add(-time.Duration(vitalsDays) * 24 * time.Hour)
		res, err := c.Reporting.Vitals.Errors.Reports.Search("apps/" + c.Package).
			Filter(fmt.Sprintf("errorIssueId = %s", lastSegment(args[0]))).
			IntervalStartTimeYear(int64(start.Year())).IntervalStartTimeMonth(int64(start.Month())).IntervalStartTimeDay(int64(start.Day())).IntervalStartTimeHours(int64(start.Hour())).IntervalStartTimeTimeZoneId("UTC").
			IntervalEndTimeYear(int64(end.Year())).IntervalEndTimeMonth(int64(end.Month())).IntervalEndTimeDay(int64(end.Day())).IntervalEndTimeHours(int64(end.Hour())).IntervalEndTimeTimeZoneId("UTC").
			PageSize(vitalsMax).Context(cmd.Context()).Do()
		if err != nil {
			return play.Wrap("search error reports", err)
		}
		var reports []*playdeveloperreporting.GooglePlayDeveloperReportingV1beta1ErrorReport = res.ErrorReports
		rows := make([][]string, 0, len(reports))
		for _, r := range reports {
			dev, osv := "", ""
			if r.DeviceModel != nil {
				dev = r.DeviceModel.MarketingName
			}
			if r.OsVersion != nil {
				osv = fmt.Sprint(r.OsVersion.ApiLevel)
			}
			rows = append(rows, []string{r.EventTime, dev, osv, play.Truncate(r.ReportText, 80)})
		}
		return emit(reports, []string{"TIME", "DEVICE", "API", "REPORT"}, rows)
	},
}

func lastSegment(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

func init() {
	vitalsCmd.PersistentFlags().IntVar(&vitalsDays, "days", 7, "look-back window in days")
	vitalsCmd.PersistentFlags().Int64Var(&vitalsMax, "max", 20, "maximum rows")
	vitalsIssuesCmd.Flags().StringVar(&vitalsType, "type", "", "CRASH | ANR (default both)")
	vitalsCmd.AddCommand(vitalsIssuesCmd, vitalsReportCmd)
	rootCmd.AddCommand(vitalsCmd)
}
