# Default sign-in: copy review record

Reviewed 2026-11-22 with Luna (`gpt-6-luna`, high reasoning, read-only).
Final English is in `locales/en.toml`; the other 11 catalogs were
translated by hand from it.

## Applied

Every Luna correction was applied except these:

- `signin_problem_passphrase`: kept "That email and passphrase don't
  match." Luna's "That passphrase didn't work" hides that a mistyped
  email is the likely cause. Only the closing "sign-in link" was applied.
- `signin_to_keymail`: kept the plan's "Continue". Luna restored
  "Continue to Keymail", which defeats making Keymail invisible.

Also applied: "sign-in link" and "latest code" used throughout, serial
commas in the passkey prompts, "Add authenticator app" for the submit
button, and the Task 2 hard-coded string at `auth/handlers.go:337`
becomes "This sign-in used a different email address. Try again."

Kept as drafted: "Sign in faster next time to {name}" (Luna: OK).

## Prompt sent

    You are a copy reviewer. Review the English UI strings below for a sign-in flow. Return, per key, either OK or a corrected string with a one-line reason. Also flag any inconsistency across strings. Follow the house rules.
    
    ## House rules (AGENTS.md, User-facing copy)
    ## User-facing copy
    
    Short sentences, plain words, and tell the reader what to do. A note
    beside a component reads "Add `Lead` to one stat in the row to make it
    big", not "One band, one lead cell. There is no second component for a
    headline stat." Both are true; only one is usable. Every note that had
    to be rewritten failed the same way — it described the design decision
    instead of the action.
    
    **The reasoning is not the copy.** Why a component works the way it does
    belongs in the code comment and the spec, where the next maintainer
    needs it. The reader of the page needs the instruction. Mixing them
    produces a page explaining itself to its own author.
    
    **Brevity is the goal; flippancy is how it fails.** Cut words, not
    seriousness. No winks, no flourishes, no sentence whose job is to sound
    good.
    
    Every user-facing string goes through the copy review before it ships,
    and in `internal/designsystem` the English is also the translation key —
    a word changed afterwards costs eleven redrafted translations. Review
    the English first, then draft the eleven.
    
    ## What a person sees (spec)
    ## What a person sees
    
    **First visit.** One email field. The address is classified on the
    server as today: a claimed Keymail address goes straight into Keymail's
    OAuth ceremony; anything else gets a magic link **(operator)**.
    
    **After the link (or Keymail), a new person goes through:**
    
    ```
    [app's sign-up step(s), if any]  →  "Sign in faster next time"  →  app
    ```
    
    - Sign-up steps are the app's own pages (name, company, anything),
      supplied to the package. None ship in the scaffold **(operator)**.
    - "Sign in faster next time" is shown once: passkey first and marked
      recommended, then authenticator app, then passphrase, and "Not now",
      which is remembered and never asks again **(operator, option A)**.
      Keymail users skip it: Keymail already secures the account with a
      passkey **(operator)**.
    
    **Returning.** The screen leads with the way this browser used last,
    and under it "Other ways to sign in" opens the chooser. This is the
    existing one-way screen (`auth/oneway.go`, the smart-door rules from
    Oficina Home's spec 2026-11-19), extended with two ways:
    
    | Way | Remembered one-tap | Chooser row |
    |---|---|---|
    | Email link / Keymail | Continue as ‹address› | Email me a link |
    | Passkey | Sign in with your passkey | Passkey |
    | Authenticator app | address shown, code field | Authenticator app |
    | Passphrase | address shown, passphrase field | Passphrase |
    
    (Copy here is placeholder; see "Copy".)
    
    **Account settings** gain a "Sign-in options" page: the ways this person
    has, add any missing one, remove any but the email link.
    
    ## Strings
    ## Copy
    
    These keys are drafts. Task 1 puts them through review and translation
    before any other task uses them. All keys are `rastrillo.ui.<key>`.
    
    | Key | Draft English |
    |---|---|
    | `signin_way_authenticator` | Authenticator app |
    | `signin_way_passphrase` | Passphrase |
    | `signin_code` | Code from your authenticator app |
    | `signin_passphrase` | Passphrase |
    | `signin_code_submit` | Sign in |
    | `signin_problem_code` | That code didn't work. Try the newest one, or email yourself a link. |
    | `signin_problem_passphrase` | That email and passphrase don't match. Try again, or email yourself a link. |
    | `signin_problem_paused` | Too many tries. Email yourself a link to sign in now. |
    | `signin_email_instead` | Email me a link instead |
    | `signin_offer_title` | Sign in faster next time to {name} |
    | `signin_offer_heading` | Sign in faster next time |
    | `signin_offer_body` | Add another way in, so you don't need an email every time. |
    | `signin_offer_recommended` | Recommended |
    | `signin_offer_passkey` | Use a passkey |
    | `signin_offer_passkey_detail` | Your face, fingerprint or screen lock. |
    | `signin_offer_authenticator` | Use an authenticator app |
    | `signin_offer_authenticator_detail` | A six-digit code from an app on your phone. |
    | `signin_offer_passphrase` | Set a passphrase |
    | `signin_offer_passphrase_detail` | A long phrase only you know. |
    | `signin_offer_later` | Not now |
    | `signin_add_passkey_heading` | Add a passkey |
    | `signin_add_passkey_body` | Your device will ask for your face, fingerprint or screen lock. |
    | `signin_add_passkey_submit` | Create a passkey |
    | `signin_add_passkey_failed` | That didn't work. Try again, or pick another way. |
    | `signin_add_authenticator_heading` | Add an authenticator app |
    | `signin_add_authenticator_body` | Scan this with your authenticator app, then type the code it shows. |
    | `signin_add_authenticator_key` | Can't scan it? Type this key instead: |
    | `signin_add_authenticator_submit` | Turn it on |
    | `signin_add_authenticator_wrong` | That code didn't match. Type the newest code the app shows. |
    | `signin_add_passphrase_heading` | Set a passphrase |
    | `signin_add_passphrase_body` | Use at least 15 characters. A few unrelated words work well. |
    | `signin_add_passphrase_short` | Use at least 15 characters. |
    | `signin_add_passphrase_long` | Use 256 characters or fewer. |
    | `signin_add_passphrase_submit` | Save passphrase |
    | `signin_added` | Done. Next time you can sign in with it. |
    | `signin_added_continue` | Continue |
    | `signin_options_title` | Sign-in options for {name} |
    | `signin_options_heading` | Sign-in options |
    | `signin_options_email` | Email link |
    | `signin_options_email_detail` | Always on. We email a link to {address}. |
    | `signin_options_add` | Add |
    | `signin_options_remove` | Remove |
    | `signin_options_removed` | Removed. |
    
    Changed keys (Task 2):
    
    | Key | Today | Draft |
    |---|---|---|
    | `signin_problem_keymail` | Keymail couldn't confirm it's you. | We couldn't confirm it's you. Try again, or email yourself a link. |
    | `signin_to_keymail` | Continue to Keymail | Continue |
    | `signin_continue_heading` | Taking you to Keymail | Signing you in |
    | `signin_continue_title` | Taking you to Keymail to sign in to {name} | Signing in to {name} |
    | `signin_continue_body` | Taking you to Keymail to confirm it's you. | Taking you to confirm it's you. |
    
    And the hard-coded string at auth/handlers.go:337 becomes "That sign-in
    came back for a different address. Try again."
    
    ---
    
    
    Note: 'signin_offer_title' and 'signin_options_title' are HTML document titles, not headings.

## Luna's whole reply

## Review

| Key | Review |
|---|---|
| `signin_way_authenticator` | OK |
| `signin_way_passphrase` | OK |
| `signin_code` | OK |
| `signin_passphrase` | OK |
| `signin_code_submit` | OK |
| `signin_problem_code` | **“That code didn’t work. Try the latest code, or email yourself a sign-in link.”** “Latest” is clearer than “newest,” and “sign-in link” says what the email contains. |
| `signin_problem_passphrase` | **“That passphrase didn’t work. Try again, or email yourself a sign-in link.”** Names the failed sign-in method directly. |
| `signin_problem_paused` | **“Too many attempts. Email yourself a sign-in link to sign in now.”** “Attempts” is more neutral than “tries”; naming the link matches the other error messages. |
| `signin_email_instead` | **“Email me a sign-in link instead”** Names what will be sent and matches the error messages. |
| `signin_offer_title` | OK |
| `signin_offer_heading` | OK |
| `signin_offer_body` | **“Add another way to sign in so you don’t need an email each time.”** “Way to sign in” is clearer than “way in.” |
| `signin_offer_recommended` | OK |
| `signin_offer_passkey` | OK |
| `signin_offer_passkey_detail` | **“Use your face, fingerprint, or screen lock.”** Adds the serial comma used in the other device prompt. |
| `signin_offer_authenticator` | OK |
| `signin_offer_authenticator_detail` | **“A six-digit code from an authenticator app.”** Names the app type consistently. |
| `signin_offer_passphrase` | OK |
| `signin_offer_passphrase_detail` | OK |
| `signin_offer_later` | OK |
| `signin_add_passkey_heading` | OK |
| `signin_add_passkey_body` | **“Your device will ask for your face, fingerprint, or screen lock.”** Matches the punctuation in the passkey detail. |
| `signin_add_passkey_submit` | OK |
| `signin_add_passkey_failed` | **“That didn’t work. Try again, or choose another way to sign in.”** Makes the next action clear. |
| `signin_add_authenticator_heading` | OK |
| `signin_add_authenticator_body` | **“Scan the code with your authenticator app, then type the code it shows.”** Identifies what “this” refers to. |
| `signin_add_authenticator_key` | **“Can’t scan the code? Type this key instead:”** Clarifies what the person may be unable to scan. |
| `signin_add_authenticator_submit` | **“Add authenticator app”** Says what this action does; “Turn it on” is vague. |
| `signin_add_authenticator_wrong` | **“That code didn’t match. Try the latest code from your app.”** Avoids repeating “code” and matches the earlier error’s “latest.” |
| `signin_add_passphrase_heading` | OK |
| `signin_add_passphrase_body` | OK |
| `signin_add_passphrase_short` | OK |
| `signin_add_passphrase_long` | OK |
| `signin_add_passphrase_submit` | OK |
| `signin_added` | OK |
| `signin_added_continue` | OK |
| `signin_options_title` | OK |
| `signin_options_heading` | OK |
| `signin_options_email` | OK |
| `signin_options_email_detail` | OK |
| `signin_options_add` | OK |
| `signin_options_remove` | OK |
| `signin_options_removed` | OK |
| `signin_problem_keymail` | **“We couldn’t confirm it’s you. Try again, or email yourself a sign-in link.”** Names the link and matches the other sign-in errors. |
| `signin_to_keymail` | **“Continue to Keymail”** Keeps the destination clear on the action button. |
| `signin_continue_heading` | OK |
| `signin_continue_title` | OK |
| `signin_continue_body` | OK |
| Hard-coded string at `auth/handlers.go:337` | **“This sign-in used a different email address. Try again.”** Makes the mismatch clear without the awkward “came back for.” |

## Consistency

Use **“sign-in link”** consistently when referring to the emailed link, and use **“latest code”** for authenticator codes. The passkey prompts should also use the same comma style: “face, fingerprint, or screen lock.”
## Task 7: Screens page, authenticator and passphrase doors (2026-11-22)

Reviewed by Luna: `codex exec -m gpt-6-luna -c model_reasoning_effort=high -s read-only --skip-git-repo-check -o task7-luna.md - < task7-prompt.md`.

### Prompt

````markdown
You are reviewing user-facing English copy for Rastrillo's design-system gallery, the "Screens" page. Each screen is a live preview of the shipped sign-in screen in one state, with a short name (a heading) and a one- or two-sentence blurb under it. The readers are developers deciding how to build their app's sign-in. These are four new screens. The English is also the translation key for eleven languages, so get the English right now.

House rules (AGENTS.md, "User-facing copy"):

> Short sentences, plain words, and tell the reader what to do. A note beside a component reads "Add `Lead` to one stat in the row to make it big", not "One band, one lead cell. There is no second component for a headline stat." Both are true; only one is usable. Every note that had to be rewritten failed the same way — it described the design decision instead of the action.
>
> **The reasoning is not the copy.** Why a component works the way it does belongs in the code comment and the spec, where the next maintainer needs it. The reader of the page needs the instruction. Mixing them produces a page explaining itself to its own author.
>
> **Brevity is the goal; flippancy is how it fails.** Cut words, not seriousness. No winks, no flourishes, no sentence whose job is to sound good.

Also: no em dashes as separators. Existing names and blurbs on the same page, for register:
- "Coming back with an email link" / "The address is on the button, so there is nothing to type."
- "A passkey" / "The fastest way in for someone who has one, and nothing to type. Offer a second way as well: people lose devices, and a passkey that will not work is a locked door."
- "An address that does not look right" / "The message belongs to the field, and that is where focus starts."
- "When Keymail could not confirm it" / "The button changes to send a link instead, so nobody is stuck."

The four new screens and what each shows:

1. Name: "An authenticator app"
   Blurb: "The address is kept from last time, so only the code is left to type. A link by email is always the way back in."
   Shows: a returning visitor who last signed in with an authenticator app. Heading "Sign in to Harbour", an Email field prefilled with grace@example.com, a field "Code from your authenticator app", a "Sign in" button, then links "Email me a sign-in link instead" and "Other ways to sign in".

2. Name: "A code that did not work"
   Blurb: "The page stays on the code with the address kept, so a fresh code is all it takes."
   Shows: the same door after a wrong code: a red callout "That code didn't work. Try the latest code, or email yourself a sign-in link." above the same prefilled Email field and an empty code field, Sign in button, and the two links.

3. Name: "A passphrase"
   Blurb: "The two fields are the ones a password manager looks for, so it can fill them both."
   Shows: a returning visitor who last used a passphrase: Email field prefilled (autocomplete="username"), a Passphrase field (type=password, autocomplete="current-password"), Sign in button, the two links.

4. Name: "A passphrase that did not match"
   Blurb: "The message does not say which half was wrong, so the page never tells anyone whether an address has an account."
   Shows: the passphrase door after a failure: callout "That email and passphrase don't match. Try again, or email yourself a sign-in link." above the prefilled Email field and an empty Passphrase field.

For each of the 8 strings, answer OK or give a corrected string in bold with a one-line reason. Keep names parallel with the existing ones. Then one line on consistency across the four.
````

### Luna's reply

1. **Name: An authenticator app** — OK. It matches the short “A passkey” pattern.
2. Blurb: **Enter the code from your authenticator app, or choose the email link to sign in another way.** This gives the reader the two available actions.
3. **Name: A code that did not work** — OK. It names the screen state in the same plain style.
4. Blurb: **Enter the latest code, or choose the email link to sign in another way.** This replaces an explanation of the page behavior with clear actions.
5. **Name: A passphrase** — OK. It matches “A passkey.”
6. Blurb: **Let your password manager fill the email and passphrase fields.** This tells builders what to support.
7. **Name: A passphrase that did not match** — OK. It follows the page’s “An address that does not look right” pattern.
8. Blurb: **Use one error for an unknown address or a wrong passphrase.** This tells builders what to show while keeping account existence private.

Across the four, the names fit the page’s short state labels; the blurbs work best when they tell builders what to offer or show.

### Applied

All four names kept as Luna passed them. All four blurbs replaced with Luna's wording. None is factually wrong: the doors do offer the code (or passphrase) and the email link, the passphrase form is built for password managers, and the error is the same for an unknown address and a wrong passphrase. The eleven translations of each blurb were redrafted from the new English (internal/designsystem/prose.go).

| Screen | Was | Now |
|---|---|---|
| signin-authenticator | The address is kept from last time, so only the code is left to type. A link by email is always the way back in. | Enter the code from your authenticator app, or choose the email link to sign in another way. |
| signin-problem-code | The page stays on the code with the address kept, so a fresh code is all it takes. | Enter the latest code, or choose the email link to sign in another way. |
| signin-passphrase | The two fields are the ones a password manager looks for, so it can fill them both. | Let your password manager fill the email and passphrase fields. |
| signin-problem-passphrase | The message does not say which half was wrong, so the page never tells anyone whether an address has an account. | Use one error for an unknown address or a wrong passphrase. |

## Task 10: Screens page, the offer and the add pages (2026-11-22)

Reviewed by Luna: `codex exec -m gpt-6-luna -c model_reasoning_effort=high -s read-only --skip-git-repo-check -o task10-luna.md - < task10-prompt.md`.

### Prompt

````markdown
You are reviewing user-facing English copy for Rastrillo's design-system gallery, the "Screens" page. Each screen is a live preview of a shipped page in one state, with a short name (a heading) and a one- or two-sentence blurb under it. The readers are developers deciding how to build their app's sign-in. These are five new screens. The English is also the translation key for eleven languages, so get the English right now.

House rules (AGENTS.md, "User-facing copy"):

> Short sentences, plain words, and tell the reader what to do. A note beside a component reads "Add `Lead` to one stat in the row to make it big", not "One band, one lead cell. There is no second component for a headline stat." Both are true; only one is usable. Every note that had to be rewritten failed the same way — it described the design decision instead of the action.
>
> **The reasoning is not the copy.** Why a component works the way it does belongs in the code comment and the spec, where the next maintainer needs it. The reader of the page needs the instruction. Mixing them produces a page explaining itself to its own author.
>
> **Brevity is the goal; flippancy is how it fails.** Cut words, not seriousness. No winks, no flourishes, no sentence whose job is to sound good.

Also: no em dashes as separators. Existing names and blurbs on the same page, for register:
- "Coming back with an email link" / "The address is on the button, so there is nothing to type."
- "A passkey" / "The fastest way in for someone who has one, and nothing to type. Offer a second way as well: people lose devices, and a passkey that will not work is a locked door."
- "An authenticator app" / "Enter the code from your authenticator app, or choose the email link to sign in another way."
- "A passphrase" / "Let your password manager fill the email and passphrase fields."
- "A passphrase that did not match" / "Use one error for an unknown address or a wrong passphrase."

What the five new screens are: after someone signs in with an emailed link for the first time, the framework shows a page offering a faster way to sign in next time. Each choice opens a page that adds that way to the account. The framework shows these pages itself; an app turns the ways on.

1. Name: "Offering a faster way in"
   Blurb: "Show this once, after a sign-in by email link. Put the passkey first, and remember Not now."
   Shows: heading "Sign in faster next time", the line "Add another way to sign in so you don't need an email each time.", then three rows: "Use a passkey" with a "Recommended" badge and "Use your face, fingerprint, or screen lock." under it; "Use an authenticator app" / "A six-digit code from an authenticator app."; "Set a passphrase" / "A long phrase only you know."; then a quiet "Not now" button. Choosing Not now is remembered and the page never comes back.

2. Name: "Adding a passkey"
   Blurb: "One button. The device asks for a face, fingerprint or screen lock."
   Shows: heading "Add a passkey", the line "Your device will ask for your face, fingerprint, or screen lock.", and a "Create a passkey" button.

3. Name: "Adding an authenticator app"
   Blurb: "Show the QR code and the key, and ask for one code before the app counts as a way in."
   Shows: heading "Add an authenticator app", "Scan the code with your authenticator app, then type the code it shows.", a QR code, "Can't scan the code? Type this key instead:" and the key, a "Code from your authenticator app" field and an "Add authenticator app" button. The authenticator is not turned on until one code from it is accepted.

4. Name: "Setting a passphrase"
   Blurb: "Ask for at least 15 characters, and nothing else about them."
   Shows: heading "Set a passphrase", "Use at least 15 characters. A few unrelated words work well.", one Passphrase field (a hidden username field carries the address for password managers), and a "Save passphrase" button. No rules about digits, symbols or capitals.

5. Name: "A way added"
   Blurb: "Say it worked, then continue to the page the person was going to."
   Shows: heading "Add a passkey", "Done. Next time you can sign in with it." and a "Continue" button that goes to the page the person was on their way to.

For each of the 10 strings, answer OK or give a corrected string in bold with a one-line reason. Keep names parallel with the existing ones and with each other. Then one line on consistency across the five.
````

### Luna's reply

1. **Name — Offering a faster way to sign in** — “Sign in” is clearer than “in” and matches the other names’ wording.
2. **Blurb — Show this after someone’s first email-link sign-in, with the passkey first. If they choose Not now, don’t show this page again.** — Makes the timing and remembered choice clear.

3. **Name — OK** — It matches the action-focused style of the other new names.
4. **Blurb — Add one button so people can create a passkey with their face, fingerprint, or screen lock.** — Tells developers what to include and how the flow works.

5. **Name — OK** — It clearly names the setup step.
6. **Blurb — Show the QR code and setup key, then ask for a code to confirm the authenticator app works.** — Makes the code’s purpose clear.

7. **Name — OK** — It clearly names the setup step.
8. **Blurb — Require at least 15 characters. Don’t require digits, symbols, or capitals.** — States the length requirement and the rules to avoid.

9. **Name — A sign-in method added** — Names the completed state and fits the style of “A passphrase that did not match.”
10. **Blurb — Confirm the sign-in method was added, then return to the page the person was trying to reach.** — Makes the confirmation and destination clear.

Across the five, the names now describe a clear step or state, and the blurbs give developers direct guidance.

### Applied

Every name and blurb as Luna gave it, with straight apostrophes, except
the offer's blurb: Luna's "after someone's first email-link sign-in" is
not quite true. The offer shows at any email-link sign-in while the
person has no faster way and has not chosen Not now, so an app that
adopts the package shows it to existing people at their next link. The
applied blurb drops "first" and keeps the rest of her wording. The
eleven translations of each string were drafted from the applied
English (internal/designsystem/prose.go), using the gallery's existing
words for passkey, passphrase and authenticator app, and each
language's own "Not now" from the locale catalogs.

| Screen | Was | Now |
|---|---|---|
| signin-offer (name) | Offering a faster way in | Offering a faster way to sign in |
| signin-offer | Show this once, after a sign-in by email link. Put the passkey first, and remember Not now. | Show this to someone after an email-link sign-in, with the passkey first. If they choose Not now, don't show this page again. |
| signin-add-passkey (name) | Adding a passkey | Adding a passkey |
| signin-add-passkey | One button. The device asks for a face, fingerprint or screen lock. | Add one button so people can create a passkey with their face, fingerprint, or screen lock. |
| signin-add-authenticator (name) | Adding an authenticator app | Adding an authenticator app |
| signin-add-authenticator | Show the QR code and the key, and ask for one code before the app counts as a way in. | Show the QR code and setup key, then ask for a code to confirm the authenticator app works. |
| signin-add-passphrase (name) | Setting a passphrase | Setting a passphrase |
| signin-add-passphrase | Ask for at least 15 characters, and nothing else about them. | Require at least 15 characters. Don't require digits, symbols, or capitals. |
| signin-added (name) | A way added | A sign-in method added |
| signin-added | Say it worked, then continue to the page the person was going to. | Confirm the sign-in method was added, then return to the page the person was trying to reach. |

## Task 10 fix round 1: the passphrase's show switch (2026-11-22)

One new key, `rastrillo.ui.signin_add_passphrase_show`, the label of
the switch under the passphrase field on the add page. Reviewed by
Luna: `codex exec -m gpt-6-luna -c model_reasoning_effort=high -s read-only --skip-git-repo-check -o luna.md - < prompt.md`.

### Prompt

````markdown
You are reviewing one new user-facing UI string for Rastrillo's default sign-in. Readers are the people signing in to an app built on it, in eleven languages besides English, so get the English right now.

House rules: short, plain words; no em dashes as separators; sentence case like the neighbouring strings.

Context: the "Set a passphrase" page. Heading "Set a passphrase", the line "Use at least 15 characters. A few unrelated words work well.", one field labelled "Passphrase" (a password-type input), then a "Save passphrase" button. The new string is the label of a checkbox, drawn as a small switch under the field, which shows the typed passphrase in plain text while it is on and hides it again when turned off. It appears only when the page's script runs.

Proposed label: "Show passphrase"

Neighbouring strings for register: "Passphrase", "Save passphrase", "Set a passphrase", "Code from your authenticator app", "Not now".

Answer OK or give a corrected string in bold with a one-line reason.
````

### Luna's reply

**Show passphrase** — OK. It’s short, clear, and matches the neighbouring strings’ sentence case.

### Applied

"Show passphrase" as proposed. Translations, using each catalog's own
word for passphrase: ga "Taispeáin an pasfhrása", zh-Hans "显示密码短语",
es "Mostrar frase de contraseña", hi "पासफ़्रेज़ दिखाएँ", pt-PT "Mostrar
frase-passe", bn "পাসফ্রেজ দেখান", ru "Показать парольную фразу", ja
"パスフレーズを表示", yue "顯示密碼短語", vi "Hiện cụm mật khẩu", ar "إظهار
عبارة المرور".

## Final review fix wave: the 500 text, the docs nav, a startup warning (2026-11-22)

The final review's finding 12: the plain-text 500 that `signin` answers
(`signin/gate.go` and `signin/offer.go`) and the two docs nav entries
the branch added had not been reviewed. The fix wave's one new
developer-facing string, the scaffold's startup warning when no
instance key is set, went in the same batch. Reviewed by Luna:
`codex exec -m gpt-6-luna -c model_reasoning_effort=high -s read-only --skip-git-repo-check -o luna.md - < prompt.md`.

### Prompt

````markdown
You are reviewing user-facing strings for Rastrillo, a Go web framework, before they ship. Return, per item, either OK or a corrected string in bold with a one-line reason. Flag any inconsistency with the neighbouring strings given.

House rules: short sentences, plain words, tell the reader what to do; brevity without flippancy; no em dashes as separators; sentence case like the neighbours.

## 1. A plain-text error page (HTTP 500)

Shown as the whole body of a plain-text page when the server fails while a signed-in person adds or removes a sign-in way, opens the "Sign in faster next time" offer, or when an app's sign-up step cannot tell whether it is still needed. The person can do nothing but retry; the details go to the operator's log. English only (it is not in the translated catalogs).

Proposed: "Something went wrong. Please try again."

Neighbours elsewhere in the framework: "Something went wrong." (the framework's generic 500), "sign-in failed" (auth's own 500), "Something went wrong. Quote the reference if you report it." (the JSON API's 500).

## 2. Documentation navigation: two new entries

Readers are developers building apps on Rastrillo, scanning the docs sidebar and the index page, where each entry shows its label and a one-line blurb.

2a. In the guides section, between "Passwords" and "Magic links".
Label: "The default sign-in"
Blurb: "A link first, a passkey, authenticator app or passphrase added after, and the last way used remembered."

Neighbours:
- "Sessions": "Signed-in state as real rows, CSRF without tokens, and step-up."
- "Passwords": "The email-and-password plugin: its two callbacks, and its shared rate limit."
- "Magic links": "Passwordless sign-in by emailed link, and the sessions.UserID trap that comes with it."
- "Passkeys": "A WebAuthn second factor at step-up and at sign-in."

2b. In the package reference section (labels are Go package names), between "sessions" and "table".
Label: "signin"
Blurb: "The default sign-in assembled: the ways in, sign-up steps, the gate, the offer and Sign-in options."

Neighbours:
- "secondfactor": "The half-session between a first factor and its session: hold, complete, strike, recovery codes."
- "sessions": "Session rows, cookies, the three middlewares, and SafeReturn."
- "table": "One table as a CSV or XLSX download, safe from CSV formula injection."

What the guide covers: an app's people sign in by emailed link first; after that first sign-in they are offered a faster way (a passkey, an authenticator app or a passphrase); the sign-in screen leads with the way that browser used last. The package reference covers the one call that assembles it, the sign-up steps an app can require, the gate that sends people to them and to the offer, the offer itself, and the Sign-in options page.

## 3. A startup warning in the server log

Read by the developer or operator running the app, once at startup, when no instance key is configured (the app then makes up a key per run, and secrets sealed under it would be unreadable after a restart, so the authenticator-app sign-in is turned off). MAGICAPP_INSTANCE_KEY stands for the app's own variable name.

Proposed: "sign-in by authenticator app is off: set MAGICAPP_INSTANCE_KEY so its secrets survive a restart"

Neighbouring log lines in the same app: "sweep spent sign-in challenges", "sweep expired sign-in links", "sweep old sign-in misses".
````

### Luna's reply

1. **OK.** It keeps the generic error’s opening and adds the retry instruction this page needs.

2a. Label: **OK.** Blurb: **Email link first, then a passkey, authenticator app or passphrase, with the last-used way remembered.** The original is hard to parse; this keeps the sequence clear and matches the neighbours’ short descriptive style.

2b. Label: **OK.** Blurb: **One call assembles default sign-in, required sign-up steps, the gate, the offer and the Sign-in options page.** This clarifies what the package covers and matches the neighbours’ concise descriptions.

3. **OK.** The lower-case opening matches the neighbouring log lines, and the message gives the operator a clear action.

### Applied

- The 500 text: "Something went wrong. Please try again." as it was.
- The guide's blurb: Luna's, verbatim.
- The package reference's blurb: Luna's, with "the" before "default
  sign-in", which her version dropped.
- Both labels and the startup warning as they were.

None of these strings is in the locale catalogs: the 500 text is a
plain-text body in English, the nav is the English docs, and the
warning is a log line.
