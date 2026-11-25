# 🤖 Getting started

This page takes you from nothing to an app serving on your machine.

## Install the CLI

```sh
go install amadan.net/rastrillo/rastrillo/cmd/rastrillo@latest
```

You need Go 1.26 or newer. [The CLI reference](/docs/cli) has every
command; you will mostly use three of them.

## Check a downloaded release

`go install` checks the module against Go's checksum database, so you
need do nothing more. Each
[release](https://amadan.net/rastrillo/rastrillo/-/releases) also has
the CLI as a tarball for macOS and Linux. To check one you downloaded,
put the release's `checksums.txt` in the same folder and run:

```sh
sha256sum --ignore-missing -c checksums.txt
```

On macOS, use `shasum -a 256` in place of `sha256sum`.

Releases after v0.47.0 also carry a CycloneDX SBOM, a list of the
modules inside, for each tarball (`rastrillo_<version>_linux_amd64.cdx.json`)
and one for the module as a whole (`rastrillo_<version>_module.cdx.json`).
`checksums.txt` covers them too. To look for known vulnerabilities, give
one to a scanner, for example `grype sbom:rastrillo_<version>_module.cdx.json`.

The build is reproducible, so you can also rebuild the binary and
compare. For the Linux amd64 tarball of v0.47.0, on a Linux amd64
machine, check which Go built it with
`go version rastrillo_v0.47.0_linux_amd64/rastrillo`, then run:

```sh
bin=$(mktemp -d)
GOTOOLCHAIN=go1.26.11 CGO_ENABLED=0 GOBIN=$bin go install -trimpath \
  amadan.net/rastrillo/rastrillo/cmd/rastrillo@v0.47.0
cmp "$bin/rastrillo" rastrillo_v0.47.0_linux_amd64/rastrillo
```

Releases after v0.47.0 are signed with Rastrillo's release key. The
signature in `checksums.txt.sig` covers `checksums.txt`, which lists the
release files. The same key signs the release's Git tag. The public key
is in the repository at `hack/allowed_signers`, with fingerprint
`SHA256:dZjcvUHtTguEVEMybnLcDLsn5U82UdNIkC6CbD1GUKA`. Save that file
beside the release's `checksums.txt` and `checksums.txt.sig`, then run:

```sh
ssh-keygen -Y verify -f allowed_signers -I release@rastrillo.org \
  -n file -s checksums.txt.sig < checksums.txt
```

The command prints `Good "file" signature for release@rastrillo.org`.
Then verify the release files against `checksums.txt` as described
above. To verify a tag in a clone of the repository, run
`hack/sign-ssh.sh verify-tag vX.Y.Z`. It accepts only an SSH signature by
`release@rastrillo.org`. Plain `git verify-tag` would also accept a GPG
signature your keyring trusts.

## Scaffold an app

```sh
rastrillo new notes
cd notes
```

`rastrillo new` writes one of three shapes, picked with `--scaffold`:

- `single`, the default: one app and its data in SQLite. This page
  follows it.
- `accounts`: `single` with sign-in by emailed link, where each person
  sees only their own rows.
- `bare`: a page and a route, with no database and no sign-in.

You get a complete app, not a stub. It compiles, its tests pass, and it
serves before you have written anything:

```text
cmd/notes/main.go       Resolve -> db.Open -> App -> Serve
internal/notes/         models, migrations, app, handlers, render, templates, static,
                        and harness + example tests, passing out of the box
manifest/               the declarative path: drop a <name>.toml here
Makefile                make ci = vet + fmt + go fix + staticcheck + lint + govulncheck + gitleaks + budget + code + routes + csp + test + perf + generate check
.amadan/ci, ci.d/       amadan runner CI, delegating to make
internal/notes/icons/   app-owned icons, edit freely
AGENTS.md               instructions + UX conventions, the source of truth
CLAUDE.md               an @AGENTS.md import, nothing more
```

Five flags shape what gets written: `--icons`, `--icon-delivery`,
`--ux`, `--theme` (`day`, `plain`, `signal`) and `--shell` (`column`,
`topbar`, `sidebar`, `console`, `stage`). The theme lands as
`static/theme.css` and the shell as `templates/layout.html`, both yours
to edit from that moment.
Take the defaults your first time through. [Icons](/docs/icons) and
[Templates](/docs/templates) explain what each one costs when you care.

The templates include an `errors.html`, wired to `Options.ErrorPage`, so
a panicking handler lands on a styled page inside your shell rather than
a blank 500. Wire the same function to `Ctx.ErrorPage` and your own
failures land there too — until you do, `view.Fail` answers plain text.

## Run it

```sh
rastrillo dev
```

This watches your source and regenerates, rebuilds and restarts on every
save. A failed build keeps the previous one serving, so a typo does not
take your app down mid-thought.

Visit the address it prints. You have a working app with CSRF, a
database, a migration ledger and a JSON API. An `accounts` app has
sign-in and sessions too.

## Where to go next

Read [The shape of an app](/docs/app-shape) before you edit anything.
It walks through the five files `new` wrote and explains why `main.go`
calls `Resolve` and `Serve` instead of `Run`.

After that the guides follow roughly the order you will need them:
[Data](/docs/data) for models and the database handle,
[Migrations](/docs/migrations) for changing the schema,
[Scoping](/docs/scoping) for keeping one user's rows away from
another's, [Forms](/docs/forms) for reading input safely, and
[Sessions](/docs/sessions) for signed-in state.

## What Rastrillo is not

It is a middle layer, not a full-stack framework. There is no ORM of its
own, no template language of its own, and no router of its own. When you
need to do something it has no opinion about, you write Go and nothing
gets in the way.
