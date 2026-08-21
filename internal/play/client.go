// Package play wraps the Android Publisher API v3 with the handful of
// behaviours gpc needs: service-account auth, the edit lifecycle (insert →
// mutate → commit, with retry on the "changed outside this Edit" conflict),
// and error translation that keeps Google's messages verbatim while adding
// the hard-won hints from real submissions.
package play

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/androidpublisher/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/playdeveloperreporting/v1beta1"
)

// Scope is the only OAuth scope the Publisher API accepts.
const Scope = androidpublisher.AndroidpublisherScope

// HTTPTimeout matches the socket timeout that made resumable AAB uploads
// survive in practice (600s).
const HTTPTimeout = 600 * time.Second

// ResolveServiceAccount applies the lookup order documented in docs/gpc-cli.md:
// --service-account flag → GPC_SERVICE_ACCOUNT env → ~/.config/gpc/service-account.json.
func ResolveServiceAccount(flag string) (string, error) {
	candidates := []string{flag, os.Getenv("GPC_SERVICE_ACCOUNT")}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "gpc", "service-account.json"))
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c, nil
		} else if c == flag || c == os.Getenv("GPC_SERVICE_ACCOUNT") {
			return "", fmt.Errorf("service account file not found: %s", c)
		}
	}
	return "", errors.New("no service account found. Pass --service-account, set GPC_SERVICE_ACCOUNT, or put the JSON at ~/.config/gpc/service-account.json\n" +
		"(Play Console → Users and permissions → invite the service account email; Google Cloud → IAM → create a JSON key. The Publisher API has no interactive login.)")
}

// Client is a thin wrapper around the generated service.
type Client struct {
	Svc       *androidpublisher.Service
	Reporting *playdeveloperreporting.Service
	Package   string
	SAPath    string
}

// New builds an authenticated client with a long HTTP timeout.
func New(ctx context.Context, saPath, pkg string) (*Client, error) {
	raw, err := os.ReadFile(saPath)
	if err != nil {
		return nil, err
	}
	// Both scopes on one token: the Publisher API and the Reporting API
	// (vitals/crashes) accept the same service account.
	creds, err := google.CredentialsFromJSONWithType(ctx, raw, google.ServiceAccount, Scope, playdeveloperreporting.PlaydeveloperreportingScope)
	if err != nil {
		return nil, fmt.Errorf("parse service account %s: %w", saPath, err)
	}
	// oauth2.NewClient takes its base transport from the context; this is how
	// the 600s timeout reaches the resumable upload requests.
	base := &http.Client{Timeout: HTTPTimeout}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, base)
	httpClient := oauth2.NewClient(ctx, creds.TokenSource)
	svc, err := androidpublisher.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	rep, err := playdeveloperreporting.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return &Client{Svc: svc, Reporting: rep, Package: pkg, SAPath: saPath}, nil
}

// EditFunc mutates an open edit. Return an error to abort (the edit is deleted).
type EditFunc func(editID string) error

// ErrEditConflict is the Console-tab-stole-the-edit condition.
var ErrEditConflict = errors.New("edit conflict")

const conflictNeedle = "outside of this Edit"

// WithEdit opens an edit, runs fn, commits, and retries the whole cycle once
// when Google reports that the app changed outside the edit (an open Console
// tab with an unsaved form does this). Set commit=false to validate only.
func (c *Client) WithEdit(ctx context.Context, commit bool, fn EditFunc) (editID string, err error) {
	const attempts = 2
	for i := 1; i <= attempts; i++ {
		editID, err = c.runEdit(ctx, commit, fn)
		if err == nil {
			return editID, nil
		}
		if i < attempts && strings.Contains(err.Error(), conflictNeedle) {
			fmt.Fprintf(os.Stderr, "gpc: edit conflict (\"%s\"), reopening edit and retrying (%d/%d)\n", conflictNeedle, i+1, attempts)
			continue
		}
		return editID, err
	}
	return editID, err
}

func (c *Client) runEdit(ctx context.Context, commit bool, fn EditFunc) (string, error) {
	edit, err := c.Svc.Edits.Insert(c.Package, &androidpublisher.AppEdit{}).Context(ctx).Do()
	if err != nil {
		return "", Wrap("open edit", err)
	}
	id := edit.Id
	if err := fn(id); err != nil {
		_ = c.Svc.Edits.Delete(c.Package, id).Context(ctx).Do()
		return id, err
	}
	if !commit {
		_ = c.Svc.Edits.Delete(c.Package, id).Context(ctx).Do()
		return id, nil
	}
	if _, err := c.Svc.Edits.Commit(c.Package, id).Context(ctx).Do(); err != nil {
		_ = c.Svc.Edits.Delete(c.Package, id).Context(ctx).Do()
		return id, Wrap("commit edit", err)
	}
	return id, nil
}

// Wrap keeps Google's error body verbatim (it is the best feedback the API
// gives) and appends the matching hint from the hard-knowledge list.
func Wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		msg = fmt.Sprintf("HTTP %d: %s", gerr.Code, gerr.Message)
		if gerr.Message == "" && gerr.Body != "" {
			msg = fmt.Sprintf("HTTP %d: %s", gerr.Code, gerr.Body)
		}
	}
	out := fmt.Sprintf("%s: %s", op, msg)
	if hint := hintFor(msg); hint != "" {
		out += "\n  hint: " + hint
	}
	return errors.New(out)
}

func hintFor(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(msg, "Only releases with status draft may be created on draft app"):
		return "the app is still a draft in Play Console (never published). Use --status draft; Console-only steps (content rating, target audience, data safety review, countries) must be finished by a human before a completed release is allowed."
	case strings.Contains(msg, conflictNeedle):
		return "a Play Console tab has an unsaved form open on this app. Close it (or discard its changes) and rerun; gpc already retried once."
	case strings.Contains(msg, "Invalid header row"):
		return "the data safety CSV header must be exactly: \"Question ID (machine readable),Response ID (machine readable),Response value,Answer requirement,Human-friendly question label\". See `gpc datasafety --help`."
	case strings.Contains(msg, "applicationNotFound") || strings.Contains(msg, "Package not found") || strings.Contains(msg, "No application was found"):
		return "the API cannot create apps. Create it in Play Console (All apps → Create app), upload one AAB there or via `gpc bundle upload` to bind the package name, and make sure the service account is invited under Users and permissions."
	case strings.Contains(msg, "Google Play Developer Reporting API has not been used") || strings.Contains(msg, "playdeveloperreporting.googleapis.com"):
		return "enable the Play Developer Reporting API in the Google Cloud project that owns this service account: https://console.cloud.google.com/apis/library/playdeveloperreporting.googleapis.com — then wait a minute and retry."
	case strings.Contains(msg, "insufficient permissions") || strings.Contains(msg, "The caller does not have permission"):
		return "invite the service account email in Play Console → Users and permissions with Release manager (or Admin) on this app; permission propagation can take a few minutes."
	case strings.Contains(lower, "version code") && strings.Contains(lower, "already been used"):
		return "bump android.versionCode (or let EAS autoIncrement) and rebuild; Play never accepts a reused versionCode."
	}
	return ""
}
