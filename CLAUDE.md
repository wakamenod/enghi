# CLAUDE.md

## Design notes

`docs/DESIGN.md` holds the design and the rationale behind it. It is kept locally and is
**not** tracked in git (see `.gitignore`), but the source comments still refer to its
section numbers. Follow it — section 3 (search) in particular is a locked specification
based on measurements, not a sketch.

## Build

The `sqlite_fts5` build tag and `CGO_ENABLED=1` are required (FTS5 and the trigram
tokenizer); `cmd/enghi/require_*.go` fails the build on purpose without them. cgo rules
out cross-compiling, so binaries are built on native runners.

```sh
make build test vet
```

## Releasing

Pushing a tag builds on a runner per OS and creates a GitHub Release with the tarballs
and `SHA256SUMS` (`.github/workflows/release.yml`).

```sh
git tag v0.1.0 && git push origin v0.1.0
```

Then update `url` and `sha256` in `packaging/homebrew/enghi.rb`, and copy the result into
`Formula/enghi.rb` in `wakamenod/homebrew-tap`.

**Before tagging, re-sign the calendar shortcut and commit it.** The signature carries an
Apple certificate that expires about a year after signing. The current one expires on
2027-10-26. A shortcut with an expired certificate no longer imports. Signing contacts
Apple and needs a Mac signed into iCloud:

```sh
python3 packaging/shortcuts/build.py   # writes and signs shortcuts/enghi-events.shortcut
```

The shortcut's window must match `WindowPastDays` and `WindowFutureDays` in
`internal/calendar/calendar.go`. After you change the shortcut, import it once and run
`shortcuts run enghi-events` to check the output. Shortcuts accepts some filters without
complaint and then ignores them.
