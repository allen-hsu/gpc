# gpc — Google Play Console CLI

**`asc` is to App Store Connect what `gpc` is to Google Play.**
A thin, agent-friendly wrapper over the Android Publisher API v3 (plus the Play
Developer Reporting API for vitals). Single static Go binary, service-account
auth, JSON when piped, `--help` that tells you which Console page each command
replaces — and which things have no API at all.

Built while shipping a real app to both stores in one day; every error hint in
here was earned.

```
$ gpc track get
TRACK       STATUS     VERSION CODES  NAME
production  draft      10             1.0.0 (10)
internal    completed  10             1.0.0
```

## Install

```sh
go install github.com/allen-hsu/gpc@latest
# or
brew install allen-hsu/tap/gpc
```

## Auth

The Publisher API has exactly one login method: a **service-account JSON key**.

1. Google Cloud → IAM & Admin → Service accounts → create one → Keys → JSON.
2. Play Console → Users and permissions → Invite user → paste the service account
   email → grant *Release manager* (or Admin) on the app.
3. Put the key where gpc looks for it, in this order:
   `--service-account <path>` → `$GPC_SERVICE_ACCOUNT` → `~/.config/gpc/service-account.json`.

```sh
export GPC_PACKAGE=com.example.app       # or pass --package / -p everywhere
gpc auth status                          # opens an edit and discards it
```

## Commands

### Shipping

```sh
gpc listing push  --dir ./play-metadata          # <locale>.json: title / shortDescription / fullDescription
gpc listing pull  --dir ./play-metadata
gpc images upload --type phoneScreenshots --locale zh-TW,en-US --path ./shots/{locale}/
gpc images upload --type icon --locale en-US --path icon.png [--replace --confirm]
gpc images list   --type phoneScreenshots --locale en-US
gpc bundle upload app.aab [--track internal --status draft --notes-dir ./notes]
gpc bundle list
gpc track set     --track internal --status draft --version-codes 10 --notes-dir ./notes
gpc track get     [--track production]
gpc track promote --from internal --to production --confirm
gpc datasafety push labels.csv        # or --no-collection for the minimal valid form
gpc datasafety template
gpc details set   --email … --website … --phone …
gpc details get
gpc mapping upload mapping.txt --version-code 10
gpc sharing upload app.aab            # internal app sharing link: no edit, no track, no review
gpc console-only                      # the list of things only a human can click
```

### Operating

```sh
gpc reviews list | get <id> | reply <id> --text … --confirm
gpc testers get | set --track internal --groups a@googlegroups.com --confirm
gpc countries get [--track production]
gpc iap list
gpc subscriptions list
gpc pricing convert 4.99 --currency USD
gpc vitals issues [--days 7 --type CRASH]
gpc vitals report <issueName>
```

### Conventions

- **Output**: table on a TTY, JSON when piped or with `--json`.
- **`--confirm`** is required for anything destructive or user-visible:
  `images --replace`, any non-`draft` release status, `reviews reply`, `testers set`.
- **`--dry-run`** on `listing push`, `images upload`, `bundle upload` performs the real
  API call inside an edit and then deletes the edit. Use it to validate payloads,
  signing and versionCodes without touching the live listing.
- **Errors**: Google's 4xx message is printed verbatim (it is the best feedback the
  API gives), with a `hint:` line appended when it matches a known trap.
- **Edit conflicts** (`A change was made to the application outside of this Edit`,
  caused by an open Console tab) are retried once automatically.

## Things you need to know about the Play API

These are baked into `--help` and the error hints, but they are worth reading once:

1. **The API cannot create an app.** Create it in Play Console; the package name is
   bound by the first AAB uploaded (Console or `gpc bundle upload`).
2. **A draft (never-published) app accepts `completed` only on the internal track.**
   production/alpha/beta answer `Only releases with status draft may be created on
   draft app.` until the Console-only checklist is done and the first review passes.
   So: `bundle upload --track internal --status completed` to test, production as draft.
3. **Data safety CSV header must be byte-exact**, or you get `Invalid header row`.
   `gpc datasafety template` prints the two-line "collects nothing" form, which is a
   complete, valid submission. For anything richer, fill the form once in Console,
   Export CSV, and push that.
4. **Legacy `inappproducts` is dead for new apps** (`403 Please migrate to the new
   publishing API`); gpc uses `monetization.onetimeproducts`.
5. **Country availability is read-only** in the API, and `internal` has none.
6. **Vitals need a second API enabled** — Play Developer Reporting API — in the GCP
   project that owns the service account. The 403 tells you the exact URL.
7. **Reviews via API = last 7 days, with comments only.** Star-only ratings are
   Console-only.
8. **Internal app sharing needs a published app** (`400 NOT_PUBLISHED` before that).

### Console-only (`gpc console-only`)

Create app · Content rating (IARC) · Target audience · Health apps declaration ·
App category · Countries/regions · Ads/News/Government/Financial declarations ·
**Send changes for review**. No endpoint exists for any of these — an agent should
prepare the answers and hand them to a human, then verify with `gpc countries get`,
`gpc track get`, `gpc testers get`.

## Listing file format

`--dir` holds one file per Play locale code (`zh-TW.json`, not `zh-Hant`):

```json
{
  "title": "≤ 30 chars",
  "shortDescription": "≤ 80 chars",
  "fullDescription": "≤ 4000 chars, emoji allowed (unlike App Store)",
  "video": "https://youtube.com/… (optional)"
}
```

Release notes for `--notes-dir` are `<locale>.txt`, ≤ 500 chars each.
Limits are checked locally (in characters, so CJK counts 1) before any edit opens.

## For agents

- `gpc <cmd> --help` states the Console page the command maps to.
- Read the verbatim 4xx before changing anything; Google names the field, the
  locale and the limit.
- Prefer `--dry-run` when unsure; prefer `draft` until a human has done the
  Console-only list; never pass `--confirm` on a user's behalf without telling them.
- Companion skills (build doctor, OTA discipline, the full Play submission flow)
  live in [mobile-shipkit](https://github.com/allen-hsu/mobile-shipkit).

## Status

Exercised end to end on a brand-new Play app (Aug 2026): `auth status`,
`bundle upload --track internal` (81 MB, committed), `track get/set/promote`,
`listing push`, `images upload`, `details set`, `datasafety push --no-collection`,
plus the refusal paths (`completed` on production of a draft app, `sharing upload`
before first publish, unknown package). Read-only commands were also run against a
live published listing. Not yet exercised for real: `mapping upload`, `reviews reply`,
`testers set`, `vitals` (needs the Reporting API enabled). Treat 0.x as "works, read
the output".

## Development

```sh
make test        # go vet + go test
make build       # bin/gpc with version from git describe
```

Releases are cut with GoReleaser on tag push (`v*`) and publish to GitHub Releases
and `allen-hsu/homebrew-tap` (formula pushed over SSH with a write deploy key stored as the `HOMEBREW_TAP_SSH_KEY` secret;
no PAT).

## License

MIT
