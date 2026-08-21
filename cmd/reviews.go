package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/api/androidpublisher/v3"

	"github.com/allen-hsu/gpc/internal/play"
)

var (
	reviewsMax   int64
	reviewsLang  string
	reviewsReply string
)

var reviewsCmd = &cobra.Command{
	Use:   "reviews",
	Short: "User reviews (list / get / reply)",
	Long: `Console page: Ratings and reviews → Reviews.

The API only returns reviews from the last 7 days that have a comment, and
only those a developer can reply to. Older or star-only ratings are Console-only.
Replies are public and can be edited (replying again replaces the text).`,
}

type reviewRow struct {
	ID         string `json:"reviewId"`
	Author     string `json:"author"`
	Stars      int64  `json:"stars"`
	Lang       string `json:"language"`
	AppVersion string `json:"appVersion"`
	Modified   string `json:"lastModified"`
	Text       string `json:"text"`
	Reply      string `json:"reply,omitempty"`
}

func flattenReview(r *androidpublisher.Review) reviewRow {
	row := reviewRow{ID: r.ReviewId, Author: r.AuthorName}
	for _, c := range r.Comments {
		if uc := c.UserComment; uc != nil {
			row.Stars, row.Lang, row.Text, row.AppVersion = uc.StarRating, uc.ReviewerLanguage, uc.Text, uc.AppVersionName
			if uc.LastModified != nil {
				row.Modified = time.Unix(uc.LastModified.Seconds, 0).UTC().Format(time.RFC3339)
			}
		}
		if dc := c.DeveloperComment; dc != nil {
			row.Reply = dc.Text
		}
	}
	return row
}

var reviewsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent reviews (last 7 days, with comments)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		call := c.Svc.Reviews.List(c.Package).MaxResults(reviewsMax)
		if reviewsLang != "" {
			call = call.TranslationLanguage(reviewsLang)
		}
		res, err := call.Context(cmd.Context()).Do()
		if err != nil {
			return play.Wrap("list reviews", err)
		}
		rows := make([]reviewRow, 0, len(res.Reviews))
		table := make([][]string, 0, len(res.Reviews))
		for _, r := range res.Reviews {
			row := flattenReview(r)
			rows = append(rows, row)
			replied := ""
			if row.Reply != "" {
				replied = "✓"
			}
			table = append(table, []string{play.Truncate(row.ID, 14), fmt.Sprint(row.Stars), row.Lang, row.AppVersion, row.Modified[:10], replied, play.Truncate(row.Text, 60)})
		}
		return emit(rows, []string{"ID", "★", "LANG", "VER", "DATE", "REPLIED", "TEXT"}, table)
	},
}

var reviewsGetCmd = &cobra.Command{
	Use:   "get <reviewId>",
	Short: "Show one review in full",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		call := c.Svc.Reviews.Get(c.Package, args[0])
		if reviewsLang != "" {
			call = call.TranslationLanguage(reviewsLang)
		}
		r, err := call.Context(cmd.Context()).Do()
		if err != nil {
			return play.Wrap("get review", err)
		}
		row := flattenReview(r)
		return emit(row, []string{"FIELD", "VALUE"}, [][]string{
			{"author", row.Author}, {"stars", fmt.Sprint(row.Stars)}, {"language", row.Lang},
			{"appVersion", row.AppVersion}, {"lastModified", row.Modified},
			{"text", row.Text}, {"reply", row.Reply},
		})
	},
}

var reviewsReplyCmd = &cobra.Command{
	Use:   "reply <reviewId> --text '…'",
	Short: "Post (or replace) the public developer reply — needs --confirm",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePackage(); err != nil {
			return err
		}
		text := strings.TrimSpace(reviewsReply)
		if text == "" {
			return fmt.Errorf("--text is required")
		}
		if n := len([]rune(text)); n > 350 {
			return fmt.Errorf("reply is %d chars (max 350)", n)
		}
		if err := requireConfirm("public reply to review " + args[0]); err != nil {
			return err
		}
		c, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := c.Svc.Reviews.Reply(c.Package, args[0], &androidpublisher.ReviewsReplyRequest{ReplyText: text}).Context(cmd.Context()).Do()
		if err != nil {
			return play.Wrap("reply review", err)
		}
		return emit(res, []string{"FIELD", "VALUE"}, [][]string{{"reviewId", args[0]}, {"reply", text}})
	},
}

func init() {
	reviewsCmd.PersistentFlags().StringVar(&reviewsLang, "translate", "", "translate review text into this language code, e.g. en")
	reviewsListCmd.Flags().Int64Var(&reviewsMax, "max", 50, "maximum number of reviews")
	reviewsReplyCmd.Flags().StringVar(&reviewsReply, "text", "", "reply text (≤ 350 chars)")
	reviewsCmd.AddCommand(reviewsListCmd, reviewsGetCmd, reviewsReplyCmd)
	rootCmd.AddCommand(reviewsCmd)
}
