# idear (in-repo half) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the framework-side half of the idear addon — the `password.ErrRefused` seam idear needs, the `docs/site/addons.md` directory page, and the `SKILL.md` pointer — so idear can be built against a framework that already admits addons exist.

**Architecture:** Three independent deliverables on one branch. `password` gains a sentinel error and a `Refuse` constructor so a `Create` callback can distinguish "I refuse this signup" from "duplicate email"; the docs corpus gains an Addons section; `SKILL.md` gains a ~330-byte pointer. No new packages, no new dependencies, and idear itself is not built here.

**Tech Stack:** Go 1.25, `net/http`, `html/template`. Docs are markdown under `docs/site/` gated by `internal/docsite`.

**Spec:** `docs/superpowers/specs/2026-08-23-idear-design.md` (§5 "The two identity adapters", §6 "How an agent gets it", §8 "The changes in this repository")

## Global Constraints

- Branch is `idear-addon`, at `c89e7d4` — `origin/main`, **v0.18.0**. **Never `git merge` to main, not even locally** — every change is a PR, squash-merged.
- All Go commands run with `GOFLAGS=-mod=mod` and `CGO_ENABLED=0`. These are set job-wide in `.github/workflows/ci.yml`; export them locally too.
- `gofmt -l .` must be empty. CI fails on any unformatted file.
- `SKILL.md` must stay **≤ 17,000 bytes** (`skillmd_test.go:34`, `skillBudget`). It is **16,129** bytes on the current base (v0.18.0). Do not raise the constant.
- Every new exported symbol in a mapped package must appear on its reference page or `internal/docsite/symbols_test.go` fails. `password` maps to `reference/password`.
- Every page on disk under `docs/site/` must be named by `docs/site/nav.json`, and every page nav names must exist — `internal/docsite/nav_test.go` checks both directions.
- Every ` ```go ` fence in `docs/site/` must parse (`internal/docsite/fences_test.go`), and every `/docs/...` link must resolve including its fragment (`internal/docsite/links_test.go`).
- Visitor-facing copy is sentence case with a full stop, matching `"That email is already registered."` and `"Enter a valid email address."`.

---

### Task 1: `password.ErrRefused` — let a Create callback refuse a signup

**Why:** `password.Config.Create`'s contract is that *any* error means duplicate-email (`password/handlers.go:260-273`). idear's `Admitting` needs to refuse an uninvited signup, and today that refusal would render as "That email is already registered." — false, and an enumeration oracle of the class PRs #69–#73 closed.

**Files:**
- Modify: `password/handlers.go` — add `ErrRefused` + `Refuse`, branch in `Signup` (the `id, err := h.cfg.Create(...)` block at `:260-273`), update the `Config.Create` doc comment (`:44-48`)
- Modify: `password/handlers_test.go` — new tests; update the `errDuplicateEmail` comment at `:24-28`, which currently states the old contract verbatim
- Modify: `docs/site/reference/password.md:39` — the `Create` paragraph
- Modify: `docs/site/passwords.md:43-45` — the same rule, restated on the guide page
- Modify: `SKILL.md:274` — "Any error from `Create` reads as a duplicate email"

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `password.ErrRefused` (`error`, sentinel for `errors.Is`) and `password.Refuse(msg string) error`. idear's `Admitting` returns `password.Refuse("New accounts on this instance are created from an invitation.")`. The refusal renders at **403** with the message shown verbatim to the visitor.

**Design note the implementer must not "simplify" away:** the refusal branch **still burns a limiter unit**. `Hash` runs before `Create` (`handlers.go:253`), so every refused attempt costs a PBKDF2; without the limiter an uninvited address is an unbounded CPU-burn vector. The reason is cost, not enumeration — unlike the duplicate-email branch, the refusal message is identical for every uninvited address and leaks nothing.

- [ ] **Step 1: Write the failing tests**

Add to `password/handlers_test.go`, after `TestSignupDuplicateEmailRerenders`:

```go
func TestSignupRefusedRendersForbidden(t *testing.T) {
	env := newTestEnv(t, func(c *password.Config) {
		c.Create = func(context.Context, string, string) (int64, error) {
			return 0, password.Refuse("New accounts here are created from an invitation.")
		}
	})

	w := httptest.NewRecorder()
	env.h.Signup(w, signupRequest("stranger@example.com", "longenoughpw"))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
	d, ok := env.signup.last()
	if !ok {
		t.Fatalf("RenderSignup not called")
	}
	if d.Error != "New accounts here are created from an invitation." {
		t.Errorf("Error = %q, want the refusal message verbatim", d.Error)
	}
	if d.Email != "stranger@example.com" {
		t.Errorf("Email = %q, want the submitted address echoed back", d.Email)
	}
}

func TestSignupRefusedIsNotTheDuplicateMessage(t *testing.T) {
	env := newTestEnv(t, func(c *password.Config) {
		c.Create = func(context.Context, string, string) (int64, error) {
			return 0, password.Refuse("By invitation only.")
		}
	})

	w := httptest.NewRecorder()
	env.h.Signup(w, signupRequest("stranger@example.com", "longenoughpw"))

	d, _ := env.signup.last()
	if strings.Contains(d.Error, "already registered") {
		t.Errorf("a refusal must not read as duplicate-email: %q", d.Error)
	}
}

func TestSignupRefusedStillCostsALimiterUnit(t *testing.T) {
	env := newTestEnv(t, func(c *password.Config) {
		c.Create = func(context.Context, string, string) (int64, error) {
			return 0, password.Refuse("By invitation only.")
		}
	})

	// Hash runs before Create, so an unbounded refusal path is a
	// PBKDF2 burn vector. Ten refusals must close the door.
	for i := 0; i < 10; i++ {
		env.h.Signup(httptest.NewRecorder(), signupRequest("stranger@example.com", "longenoughpw"))
	}

	w := httptest.NewRecorder()
	env.h.Signup(w, signupRequest("stranger@example.com", "longenoughpw"))
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status after 10 refusals = %d, want 429", w.Code)
	}
}

func TestSignupPlainErrorStillReadsAsDuplicate(t *testing.T) {
	env := newTestEnv(t, nil)
	hash, _ := password.Hash("s3cretpw")
	env.store.create(context.Background(), "taken@example.com", hash)

	w := httptest.NewRecorder()
	env.h.Signup(w, signupRequest("taken@example.com", "anotherlongpw"))

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 — a non-refusal error keeps the old contract", w.Code)
	}
	d, _ := env.signup.last()
	if d.Error != "That email is already registered." {
		t.Errorf("Error = %q, want the duplicate message", d.Error)
	}
}
```

`newTestEnv(t, mut func(*password.Config))` already takes a mutator (`password/handlers_test.go:152`), so these tests need **no change to the helper** — they pass a closure that swaps `Create`. Do not add a `rebuild` method.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./password/ -run 'TestSignupRefused|TestSignupPlainError' -count=1 -v
```

Expected: FAIL — `undefined: password.Refuse`.

- [ ] **Step 3: Add the sentinel and constructor**

In `password/handlers.go`, after the `wrongCredentials` const block:

```go
// ErrRefused marks a Create failure as a policy refusal rather than a
// storage failure. Signup renders a wrapped error's message to the
// visitor at 403 instead of the duplicate-email copy at 422.
//
// A membership layer is the motivating caller: refusing an uninvited
// signup with the duplicate-email message is both false and an
// enumeration oracle. Use Refuse to build one.
var ErrRefused = errors.New("rastrillo/password: signup refused")

// Refuse wraps a visitor-facing message as a signup refusal. The
// message is rendered verbatim, so write it as visitor copy — sentence
// case, ending in a full stop.
func Refuse(msg string) error { return &refusal{msg: msg} }

type refusal struct{ msg string }

func (r *refusal) Error() string { return r.msg }
func (r *refusal) Unwrap() error { return ErrRefused }
```

- [ ] **Step 4: Branch in `Signup`**

Replace the `id, err := h.cfg.Create(...)` block (`handlers.go:260-273`) with:

```go
	id, err := h.cfg.Create(r.Context(), email, hash)
	if errors.Is(err, ErrRefused) {
		// A refusal is policy, not storage: the visitor hears the
		// plugin's own reason at 403. It still costs a limiter unit —
		// Hash ran above, so an unbounded refusal path is a PBKDF2
		// burn vector. Unlike the duplicate branch below, the message
		// is identical for every refused address and leaks nothing.
		h.limit.fail(email, time.Now())
		w.WriteHeader(http.StatusForbidden)
		h.cfg.RenderSignup(w, r, PageData{
			Error:    err.Error(),
			Email:    email,
			ReturnTo: r.FormValue("return_to"),
		})
		return
	}
	if err != nil {
		// Create's only realistic failure against a unique-email store
		// is a duplicate; any error is reported that way to the visitor
		// rather than leaking storage details, but it's still logged —
		// a DB outage should not vanish silently just because it looks
		// like a duplicate-email case to the caller.
		h.cfg.Logger.Error("rastrillo/password: create", "err", err)
		// The honest answer is also the oracle (see the doc comment),
		// so it costs a limiter unit: ten confirmations of one address
		// inside the window and both doors block.
		h.limit.fail(email, time.Now())
		h.rerenderSignup(w, r, "That email is already registered.", email)
		return
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./password/ -count=1
```

Expected: PASS, all 19 tests.

- [ ] **Step 6: Update the four places that state the old contract**

`password/handlers.go`, the `Config.Create` doc comment — replace "Any error Create returns is treated as a duplicate email" with:

```go
	// Create stores a new user's email and password hash, returning the
	// new id. Nil disables signup entirely: SignupPage and Signup both
	// answer 404. An error wrapping ErrRefused is a policy refusal and
	// renders its own message at 403; any other error is treated as a
	// duplicate email (the only realistic failure mode for a
	// unique-email store) and re-renders accordingly.
```

`password/handlers_test.go:24-28`, the `errDuplicateEmail` comment:

```go
// errDuplicateEmail is the fake store's stand-in for whatever
// distinct error the app's real Create returns on a UNIQUE
// constraint violation — handlers.go's contract is: any error from
// Create that does NOT wrap ErrRefused re-renders the duplicate-email
// message, no sentinel type required for that path.
```

`docs/site/reference/password.md:39` — the live text is now:

> `Create` stores a new user. Any error it returns reads as a duplicate
> email, the only realistic failure for a unique-email store. Leave it nil
> and signup is disabled entirely: `SignupPage` and `Signup` both 404.

Replace it with (keeping the corpus voice — no bold shouting, "Leave it nil"
phrasing preserved):

```markdown
`Create` stores a new user. An error wrapping `ErrRefused` is a policy
refusal: `Signup` renders that error's message verbatim at 403. Any other
error reads as a duplicate email, the only realistic failure for a
unique-email store. Leave it nil and signup is disabled entirely:
`SignupPage` and `Signup` both 404.

```go
var ErrRefused = errors.New("rastrillo/password: signup refused")

func Refuse(msg string) error
```

`Refuse` builds a refusal carrying visitor copy. A membership layer is
the motivating caller — refusing an uninvited signup with the
duplicate-email message is both false and an enumeration oracle. The
refusal still costs a rate-limiter unit, because `Hash` runs before
`Create`.
```

`docs/site/passwords.md:43-45` — the live text is now:

> `Create` stores a new user and returns the id. Any error it returns is
> read as a duplicate email, which is the only realistic failure for a
> unique-email store.

Rewrite so any error *other than one wrapping `password.ErrRefused`* reads as
a duplicate email, and say what a refusal does instead. The paragraph two
below it already ends "Handy for an invite-only app." — an invite-only app is
exactly the `ErrRefused` caller, so make the two read as one thought. Keep
the corpus voice and the ~72-column width.

`SKILL.md:274` — replace "Any error from `Create` reads as a duplicate" so it reads:

```
Any error from `Create` reads as a duplicate email, unless it wraps
`password.ErrRefused` (use `password.Refuse(msg)`), which renders that
message at 403.
```

- [ ] **Step 7: Run the full gate**

```bash
gofmt -l . && GOFLAGS=-mod=mod CGO_ENABLED=0 go build ./... && GOFLAGS=-mod=mod CGO_ENABLED=0 go vet ./... && GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./... -count=1
```

Expected: `gofmt -l .` prints nothing; all tests pass, **including `internal/docsite` `TestExportedSymbolsAreDocumented`** (which now sees `ErrRefused` and `Refuse` and requires both on `reference/password`) and `TestSkillMDStaysWithinBudget`.

- [ ] **Step 8: Commit**

```bash
git add password/handlers.go password/handlers_test.go docs/site/reference/password.md docs/site/passwords.md SKILL.md
git commit -m "password: let a Create callback refuse a signup

Create's contract turned any error into \"That email is already
registered.\" — false for a policy refusal, and an enumeration oracle.
An error wrapping ErrRefused now renders its own message at 403.

It still costs a limiter unit: Hash runs before Create, so an
unbounded refusal path is a PBKDF2 burn vector.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: The addons directory page

**Why:** Spec §6 — an addon whose skill doc an agent cannot find saves an app the typing and none of the reading. This page's job is to hand an agent a fetchable skill, not to describe one.

**Files:**
- Create: `docs/site/addons.md`
- Modify: `docs/site/nav.json` — a new section between "Building an app" and "Reference"

**Interfaces:**
- Consumes: `password.Refuse` from Task 1 (named in the idear wiring snippet).
- Produces: the slug `addons`, linked from `SKILL.md` in Task 3.

- [ ] **Step 1: Write the failing nav test**

Add the nav entry first, so the gate fails on the missing file rather than the other way round. In `docs/site/nav.json`, insert this section object between the `"Building an app"` section and the `"Reference"` section:

```json
    {
      "title": "Addons",
      "pages": [
        {
          "slug": "addons",
          "label": "Addons",
          "blurb": "Modules that ship outside the framework, the four rules they follow, and how to fetch their skills."
        }
      ]
    },
```

- [ ] **Step 2: Run the nav gate to verify it fails**

```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/docsite/ -run TestNavAndFilesAgreeBothWays -count=1 -v
```

Expected: FAIL — `Load` errors because `docs/site/addons.md` does not exist.

- [ ] **Step 3: Write the page**

Create `docs/site/addons.md`:

````markdown
# 🤖 Addons

Rastrillo's core holds what every app needs and what is hard to get
right twice. Some things are neither — real for many apps, wrong for
every app to carry. Those ship as **addons**: separate modules,
versioned separately, that an app pulls in when it wants them.

An addon is not a plugin system. There is no registry, no lifecycle and
no hook table. An addon is an ordinary Go module that happens to obey
four rules.

## What makes an addon

**It depends on Rastrillo; Rastrillo never depends on it.** The arrow
points one way, always. Nothing in the framework knows an addon exists,
which is what lets an addon ship on its own schedule.

**It ships its own migrations, namespaced.** A `migrate.Set` the app
merges into `BootSchema` — never into its own `Schema`, or
`rastrillo migration check` proposes dropping tables that `Models` does
not know about. See [Migrations](/docs/migrations).

**It ships its own `SKILL.md`, fetchable over HTTP.** The framework's
sits at the repo root a scaffolded app points to; an addon's would
otherwise land in a module-cache directory nobody names. An addon that
an agent cannot read is an addon that saves the typing and none of the
reading.

**It does not re-implement the core.** Sessions, CSRF, migrations,
forms and flash are already there. An addon that brings its own is a
fork wearing a smaller name.

## The directory

### idear — accounts, roles, invitations

**Module:** `amadan.net/rastrillo/idear` ·
**Source:** <https://amadan.net/rastrillo/idear>

The roster for an instance: who is in it, at what role, and who may
change that. Three strictly ordered roles — Owner, Admin, Member, with
exactly one Owner at all times — plus invitations, member management,
and the middleware that makes the membership gate the short path.

Load its authoring doc before building on it:

```sh
curl -s https://amadan.net/rastrillo/idear/SKILL.md
```

It sits on top of [sessions](/docs/sessions) and whichever identity
plugin the app already chose, so [passwords](/docs/passwords) and
[magic links](/docs/magic-links) both keep working:

```go
r, err := idear.New(idear.Config{DB: d.G, OpenSignUp: false})
if err != nil {
	return nil, err
}
ph, err := password.New(password.Config{
	Sessions: sess,
	Lookup:   lookupUser(d.G),
	Create:   r.Admitting(createUser(d.G)),
})
```

**What it deliberately does not do.** It is not an identity provider: it
never mints a session, never hashes a password, never renders a sign-in
form. It has no tenant field and no tenant scope — a CARLOS app serves
one team, and separating teams stays the platform's job. See
[Scoping](/docs/scoping).

## Publishing an addon

Follow the four rules above, then serve `SKILL.md` at a stable URL and
send a patch adding an entry here. An addon nobody can find and no agent
can read is a library, not an addon.
````

- [ ] **Step 4: Run the docs gates to verify they pass**

```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/docsite/ -count=1 -v
```

Expected: PASS. Specifically `TestNavAndFilesAgreeBothWays`, `TestInternalLinksResolve` (the four `/docs/...` links resolve to existing slugs), `TestAnchorsAreUnique`, and `TestGoFencesParse` (the one ` ```go ` fence parses as a statement list; the ` ```sh ` fence is not checked).

- [ ] **Step 5: Commit**

```bash
git add docs/site/addons.md docs/site/nav.json
git commit -m "Docs: an addons directory, and the four rules an addon follows

The framework's SKILL.md sits where a scaffolded app points; an addon's
lands in a module cache nobody names. This page's job is to hand an
agent a fetchable skill, not to describe one.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: The `SKILL.md` pointer

**Why:** Spec §8. `SKILL.md` §3 tells a reader that scoping separates users and not tenants; the very next question is how roles work, and today the file has no answer.

**Files:**
- Modify: `SKILL.md` — §3 Scoping, after the paragraph beginning "Scoping separates *users* within one instance"

**Interfaces:**
- Consumes: the `addons` slug from Task 2.
- Produces: nothing.

- [ ] **Step 1: Record the current size**

```bash
wc -c SKILL.md
```

Expected: 16,129 (the v0.18.0 base) plus whatever Task 1's §5 edit added, roughly +60. Record the actual number — Step 4 checks the delta against it, not against a remembered constant.

- [ ] **Step 2: Add the pointer**

In `SKILL.md` §3, immediately after the paragraph ending "isolation is the platform's process-and-file boundary, not a WHERE clause.", insert:

```markdown
**Roles and membership are an addon, not core.** Rastrillo has no role
concept: who is *in* this instance and at what rank is
`amadan.net/rastrillo/idear` — Owner/Admin/Member, invitations, and the
members UI, over `sessions` and either identity plugin.
Full treatment: docs/site/addons.md — rastrillo.org/docs/addons
```

- [ ] **Step 3: Verify the budget gate passes**

```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test . -run TestSkillMDStaysWithinBudget -count=1 -v
```

Expected: PASS. If it fails, **trim; do not raise `skillBudget`** — the constant's own doc comment says raising it is a product decision, not a convenience.

- [ ] **Step 4: Confirm the size landed where the spec predicted**

```bash
wc -c SKILL.md
```

Expected: roughly 16,520, comfortably under the 17,000 gate. A number above 16,700 means the insert grew in editing — re-read it against the spec's §8 quote.

- [ ] **Step 5: Run the full gate**

```bash
gofmt -l . && GOFLAGS=-mod=mod CGO_ENABLED=0 go build ./... && GOFLAGS=-mod=mod CGO_ENABLED=0 go vet ./... && GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./... -count=1
```

Expected: everything passes.

- [ ] **Step 6: Commit and open the PR**

```bash
git add SKILL.md
git commit -m "SKILL.md: name the addon that owns roles and membership

Section 3 tells a reader scoping separates users and not tenants. The
next question is how roles work, and the file had no answer.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
git push -u origin idear-addon
gh pr create --title "Addons: the directory, the password refusal seam, and the idear pointer" --body "$(cat <<'BODY'
The framework half of the idear addon (spec:
`docs/superpowers/specs/2026-08-23-idear-design.md`).

- `password.ErrRefused` + `Refuse(msg)`: a Create callback can refuse a
  signup and have its own reason rendered at 403, instead of the
  duplicate-email message at 422. Still costs a limiter unit — Hash runs
  before Create, so an unbounded refusal path is a PBKDF2 burn vector.
- `docs/site/addons.md`: the addons directory, the four rules, and the
  `curl` line that hands an agent idear's SKILL.md.
- `SKILL.md` §3: a pointer to it, ~330 bytes, no trim needed.

idear itself is not in this PR.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
BODY
)"
```

---

## Self-Review

**Spec coverage.** §5's `password.ErrRefused` → Task 1. §6's skill-delivery URL → Task 2 (the `curl` line). §8's three documentation changes → Tasks 1 (the four doc edits), 2 (addons page + nav) and 3 (`SKILL.md` pointer). §9's sequencing — in-repo docs first, idear second — is honoured by this plan covering only the in-repo half.

**Deliberately out of scope, and tracked in the idear plan, not here:** every §4/§5 library concern (the `Member`/`Invitation` schema, `CarryToken`, the reconciliation route, the CAS on `Accept`, `Reactivate`, the `NotFound` hook) and §9's `go get` vanity-path smoke test, which is task zero *there* because nothing here depends on the path resolving.

**Type consistency.** `password.Refuse(msg string) error` and `password.ErrRefused` are spelled identically in Task 1's tests, implementation, doc comment, reference page and Task 2's prose. `idear.New(idear.Config{...})` and `r.Admitting(...)` in Task 2's snippet match the spec §5 signatures exactly.

**Verified against the source, not assumed.** `newTestEnv`'s mutator parameter (`handlers_test.go:152`), the `Signup` error branch (`handlers.go:264-273`), `rerenderSignup`'s hardcoded 422 (`handlers.go:280`), the `referencePages` map entry `"password": "reference/password"` (`internal/docsite/symbols_test.go:35`), `skillBudget = 17_000` with the file at 16,068, and the §3 anchor paragraph ending "not a WHERE clause." at `SKILL.md:162`.

**One thing the executor must not paper over.** If a gate fails, fix the change — never the gate. That means: do not raise `skillBudget`, do not add a `docs:ignore` marker to silence `symbols_test`, and do not weaken a test to fit an implementation that surprised you.
