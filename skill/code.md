# Code that reads like a sentence

Write Go to fourteen rules and four measured limits.

## The rules

1. **Spell names out, everywhere, except `err`, `ctx` and Go's testing
   values (`t`, `b`, `f`).** `response`, not `w`; `request`, not `r`;
   `app`, not `a`; `tag`, not `t`. `ctx` stays because `context` is the
   package. Type parameters may be one capital (`Opt[T]`).
2. **Name things for what the person is doing, not how the code does
   it.** Not `saveEntry`, but `entries.Publish` and `entries.SaveDraft`,
   one per button.
3. **One route, one intent.** No `isNew` or `intent` switch inside a
   handler. Two buttons that mean two things post to two routes; in one
   form, each button's `formaction` sends it to its own route, with no
   JavaScript: `<button formaction="/entries/7/publish">`.
4. **Every form action has four beats:** read the input, run one
   operation, answer a refusal, say what success looks like. Nothing
   else goes in an action. Input that does not parse is a refusal from
   the first beat and never reaches the operation.
5. **Operations hold the rules and the database work.** They return the
   `op` refusals (`Invalid`, `NotFound`, `Forbidden`, `Conflict`) and
   never touch HTTP, so they can be tested without a request.
6. **A helper answers refusals; success stays written out** in the
   action, because it is the line a reader most wants to see.
7. **Comments say why, in one or two full sentences.** Why something
   deliberately looks wrong, or a rule that spans files. Never what the
   code does. Long reasoning goes in the spec or the commit, and the
   comment points there: `// NotFound, not Forbidden: a caller must not
   learn that the row exists.`
8. **One way to do each thing.** Agents copy the nearest example, so
   three patterns means a random pick. For routing, the one way is the
   route table, below.
9. **One canonical example app follows every rule.** Until it lands,
   copy the action below, from `docs/site/reference/action.md`.
10. **Names you can grep and land in one place.** `tagUpdate` and
    `access.UpdateTag` each find one definition; a `Handle` in every
    action file finds dozens. No `util` packages.
11. **Explicit over magic.** No `init()` registration, reflection-based
    dispatch or struct-tag mini-languages where plain code would do.
    Go-to-definition must work in an editor and with grep.
12. **Types that rule out mistakes.** `op.Opt` is the model. Where a
    mix-up is plausible, type the ID: `type TagName string`, not
    `string`.
13. **Error messages say the fix:** what is wrong, where, and what to
    change.
14. **Tests are named as sentences and run fast.**

## An action

```go
type tagForm struct{ parsed *form.Parsed }

func (again tagForm) ShowAgain(
	visit *action.Visit, status int, errors form.Errors,
) {
	visit.Render("tag_edit", status, map[string]any{
		"Echo": again.parsed.Echo(), "Errors": errors,
	})
}

func (app *App) tagRename(visit *action.Visit) {
	parsed, err := visit.ReadForm(form.Field{Name: "Name", Required: true})
	if visit.RefusedShowing(err, tagForm{parsed}) {
		return
	}
	id, err := visit.PathID("id")
	if visit.Refused(err) {
		return
	}
	err = app.tags.Rename(visit.Context(), id, parsed.String("Name"))
	if visit.RefusedShowing(err, tagForm{parsed}) {
		return
	}
	visit.Flash("Tag renamed.")
	visit.Redirect(tagPage.For(id))
}
```

Read the form first, so a broken body is a 400 before any path or
database work. `RefusedShowing` shows the form again at 422 for
`op.Invalid` and 409 for `op.Conflict`, and the rest as `Refused` does
(404, 403, 500). Use `Refused` on a page with no form; `RefusedShowing`
panics on a nil form. If an action outgrows 15 lines, the extra work
belongs in the operation.

## Routes

One `func Routes(routes *action.Routes, app *App)` under `internal/`
registers every route; the app serves with
`Routes(action.NewRoutes(router, app.contextFor), app)`. It only registers:
`rastrillo routes` calls it with an empty `App`, and `--check` fails an
action no route mounts. Paths are full, inside `Route` too:
`Route("/admin", ...)` holds `"/admin/tags"`. A named `Path` registers
and fills:
`const tagPage action.Path = "/tags/{id}"`;
`routes.Get(tagPage, app.tagShow)`;
`routes.Post(tagPage, app.tagRename)`.

## An operation

```go
func (tags *Tags) Rename(ctx context.Context, id int64, name string) error {
	if strings.TrimSpace(name) == "" {
		return op.Invalid{Fields: map[string]string{"Name": "Enter a name."}}
	}
	result := tags.database.WithContext(ctx).
		Model(&Tag{ID: id}).Update("Name", name)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return op.NotFound{}
	}
	return nil
}
```

## The limits, as measured

`rastrillo budget code [dir]` measures the module at `dir` (default `.`).

- **columns:** a line over 80 columns, a tab moving to the next stop of
  4, each rune one column. Every Go file, tests included.
- **file:** a non-test `.go` file over 400 lines, counted as `wc -l`
  does, plus one for a last line with no newline.
- **func:** a body over 40 lines, or 15 for an action: the lines
  strictly between the line of the opening brace and the line of the
  closing one, so a wrapped signature is free. Non-test files only.
  Measured alone and inside what holds it: every function and method;
  a literal assigned at package level, named by its variable; a literal
  nested elsewhere in a package-level variable's value,
  `<variable>.func<N>`; an action literal anywhere,
  `<enclosing>.action<N>`. N counts from 1 in source order.
- **An action** has exactly one parameter, a `*Visit` from
  `amadan.net/rastrillo/rastrillo/action` under any import name, and
  no results. A type alias of `Visit` is not followed.
- **names:** a one-letter name the file declares: `var`, `:=`, `range`,
  a type switch's, a constant, a parameter, a receiver, a named result.
  Allowed: `_`; `t` typed `*testing.T` or `testing.TB`; `b` typed
  `*testing.B`; `f` typed `*testing.F`; a one-capital type parameter.
  Struct fields, labels, and type and function names are not counted.
  Every Go file, tests included.

Not measured: generated files (`// Code generated ... DO NOT EDIT.`
before the package clause); `testdata`, `vendor`, `node_modules`,
directories starting `.` or `_`, and nested modules, each measured
from its own root. A file whose comment before the package clause
holds `//rastrillo:data <reason>` is a table of words or values, exempt
from columns and file, never from func or names.

## The records

`.rastrillo/code.txt` holds the offences the code had when the gate
began, one a line: `<kind> <path> <key, or -> <count> [reason]`, as in
`func api/decode.go Decode 43`. A columns key is the first 12 hex
digits of the SHA-256 of the line with trailing white space removed
(leading kept), counting identical lines. A names key is
`function/name` (`-/name` at package level), counting repeats. A func
key is the name (`Name`, a method's `Recv.Name`, or as above), its
count the body's ceiling. A file key is `-`, its count the ceiling.

So editing or re-indenting a recorded wide line makes it new: wrap it.
A renamed, moved or split function or file is new: it must fit.

Records only go down. `--lower` lowers and deletes records to match,
never adds or raises one. `--init` records every offence and refuses if
the file exists. A record you add or raise needs a reason after its
count: the gate compares with the file at
`git merge-base HEAD origin/main`, and a reviewer judges the reason.
Fix the code first.

## What the gate says

| It says | It means |
|---|---|
| `over the 80 allowed` (or 400, 40, 15), `is a single-letter name` | A new offence: fix the code. |
| `allows C. Wrap the new one.` or `Name the new one` | A copy of a recorded line or name. |
| `is now N lines, over the C its record ... allows` | A recorded function or file grew: shorten it. |
| `Lower the record to N` | The code improved: run `--lower`, commit `code.txt`. |
| `has nothing left to record` | Fixed or gone: delete it, or run `--lower`. |
| `is new since the base` or `is raised from C to N`, `and has no reason` | Fix the code, or write why after the count. |
| `is not a kind`, `is not a count`, `want <kind> ...`, a key, `a second record` | A bad `code.txt` line: fix it as told. |
| `cannot find the base` | Run it in a git checkout; fix git, or fetch full history. |
| `--init left it alone` | The file exists: run `--lower`. |

Notes, not failures: `No origin/main to compare with` and `The base
has no .rastrillo/code.txt` skip the reason check. `This checkout is
shallow` compares with origin/main's tip: if a raise is only main
moving on, merge origin/main.

It exits 0 within the limits, 1 over one, 2 when `code.txt` cannot be
read or `--init` finds it. A new app's `make ci` runs it as
`make code`. An older app adopts it with `--init` and a `code` lane in
`make ci`, and adds `routes --check` only once it has a `Routes`.
