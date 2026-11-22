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

## Follow-ups: `--auth=signin` and the QR code's label (2026-11-22)

Reviewed with Luna (`gpt-6-luna`, high reasoning, read-only): the
`--auth` flag's help, the docs and changelog lines that teach
`--auth=signin`, and the new locale key
`rastrillo.ui.signin_add_authenticator_qr`, the QR code's accessible
label on the add page, which replaces the English `totp` baked in.

### Prompt

    You are a copy reviewer. Review the English strings below. Return, per item, either OK or a corrected string with a one-line reason. Follow the house rules.
    
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
    
    ## Context
    
    Rastrillo is a Go web framework. `rastrillo new --auth=<kind> <name>` scaffolds an app. The kind that gives an app sign-in was called `magic` (for "magic link"); it now gives the whole default sign-in: an emailed link first, then the person can add a passkey, an authenticator app or a passphrase. It is being renamed `signin`; `magic` stays accepted as an old name. `none` (the default) gives no sign-in. `--scaffold accounts` is the same app as `--auth=signin`.
    
    ## Items
    
    1. CLI flag help for `--auth` (Go's flag package prints it under `-auth string`, followed by ` (default "none")`). Requirement: lists signin first, says in one plain sentence what signin gives, says magic is the old name:
    
       sign-in the app starts with: none, or signin, which signs people in by emailed link and lets them add a passkey, authenticator app or passphrase (magic is its old name); accounts starts with signin
    
    2. docs/site/cli.md, a sentence after the paragraph on what `--scaffold accounts` writes:
    
       `--auth=magic` is the old name for `--auth=signin` and still works.
    
    3. CHANGELOG.md, Unreleased, heading and bullet:
    
       ### Changed: `--auth=signin`
       - `rastrillo new --auth=signin` starts an app with the default sign-in: an emailed link, then a passkey, authenticator app or passphrase. `--auth=magic` is its old name and writes the same app.
    
    4. Accessible label (aria-label) on the QR code image on the "Add an authenticator app" page. Neighbouring strings on that page: heading "Add an authenticator app"; body "Scan the code with your authenticator app, then type the code it shows."; "Can't scan the code? Type this key instead:". Draft:
    
       QR code for your authenticator app
    
    5. docs/site/reference/totp.md, after the paragraph saying `Enrolment.QR` is the otpauth URI as an inline SVG:
    
       `QR`'s screen-reader label is English. On a page in another language, call `LabelledQR` with the label in that language.
    
    6. CHANGELOG.md, Unreleased, a second bullet:
    
       - The authenticator app's QR code on the add page is labelled for screen readers in the page's language. `totp.Enrolment.LabelledQR` draws the QR with a label you give. `signin.Add` now holds `URI`, and `Add.QR(label)` draws it.

### Luna's reply

1. **Corrected:** `signin starts with an emailed link and lets people add a passkey, authenticator app or passphrase; magic is its old name; none gives no sign-in` — Puts `signin` first and states what each option does plainly.

2. **OK**

3. **Corrected:**
   ```markdown
   ### Changed: `--auth=signin`
   - `rastrillo new --auth=signin` starts an app with sign-in by emailed link. People can add a passkey, authenticator app or passphrase. `--auth=magic` is the old name and creates the same app.
   ```
   — Separates the initial sign-in method from the options people can add.

4. **OK**

5. **Corrected:** `The screen-reader label for \`Enrolment.QR\` is in English. On pages in another language, call \`LabelledQR\` and pass a label in that language.` — Makes the API reference clear and gives the reader a direct instruction.

6. **Corrected:** `The add page's authenticator app QR code now has a screen-reader label in the page's language. \`totp.Enrolment.LabelledQR\` draws a QR code with a label you provide. \`signin.Add\` now exposes \`URI\` and \`Add.QR(label)\`.` — Clarifies what each API provides.
### Applied

- 1, the flag's help: Luna's, verbatim. It drops the old help's
  "accounts starts with signin"; `--scaffold`'s own help already says
  accounts is single with sign-in.
- 2 and 4: as drafted (OK).
- 3, the changelog bullet: Luna's, verbatim.
- 5, the totp reference: Luna's, with `LabelledQR` written
  `Enrolment.LabelledQR`, the name the docs symbol check looks for.
- 6, the changelog's QR bullet: Luna's, verbatim.

The QR label's 11 translations were drafted by hand from the English
and are part of the translation review below.

## Translation review: ga, bn, hi, yue, ru, zh-Hans (2026-11-22)

This is a model review, not a native speaker's. GPT-6 Astra
(`gpt-6-astra`, high reasoning, read-only) read the English and the six
catalogs' values for every sign-in key 6cf85d05 added or changed, plus
`signin_add_authenticator_qr`, and was asked for corrections only where
meaning, register or terminology is wrong. No native speaker of these
six languages has reviewed them yet.

### Prompt

    You are reviewing UI translations for a web app's sign-in flow. The English is the source and is final. Below are the English strings and their translations into six languages: ga (Irish), bn (Bengali), hi (Hindi), yue (Cantonese, Traditional script, Hong Kong usage), ru (Russian), zh-Hans (Simplified Chinese).
    
    Return corrections ONLY where a translation's meaning, register or terminology is wrong: it says something the English does not, misses part of what the English says, uses the wrong level of formality for a consumer web app (be consistent with the rest of that language's strings), or uses a term inconsistently with the other strings in the same language (for example "authenticator app", "passkey", "passphrase", "sign-in link", "code"). Do not rewrite for style alone. Keep every placeholder in braces ({name}, {address}) exactly as written.
    
    Context that matters:
    - An "authenticator app" is any TOTP app; the English no longer says it is on a phone, so a translation that says "phone" is wrong.
    - "Passkey" is the WebAuthn credential; "passphrase" is a long password the person chooses.
    - "Sign-in link" is the link emailed to the person. "Keymail" is not named on screen any more.
    - `rastrillo.ui.signin_add_authenticator_qr` is a screen reader's label for the QR code image.
    - Buttons are short imperatives; `signin_options_add` and `signin_options_remove` are buttons in a list of sign-in ways; `signin_options_removed` is a status line after removing one.
    
    Answer format, one line per correction, nothing else:
    <lang> <key>: <corrected string> — <one-line reason>
    If a language needs no corrections, write "<lang>: no corrections".
    
    
    ## rastrillo.ui.signin_problem_keymail
    en: We couldn't confirm it's you. Try again, or email yourself a sign-in link.
    ga: Níorbh fhéidir linn a dheimhniú gur tusa atá ann. Bain triail eile as, nó seol nasc síniúcháin isteach chugat féin trí ríomhphost.
    bn: আমরা নিশ্চিত করতে পারিনি যে এটি আপনিই। আবার চেষ্টা করুন, বা নিজেকে একটি সাইন-ইন লিঙ্ক ইমেল করুন।
    hi: हम पुष्टि नहीं कर सके कि यह आप ही हैं। फिर कोशिश करें, या खुद को साइन-इन लिंक ईमेल करें।
    yue: 我哋確認唔到係你本人。請再試一次，或者寄一條登入連結俾自己。
    ru: Нам не удалось подтвердить, что это вы. Попробуйте снова или отправьте себе ссылку для входа.
    zh-Hans: 我们无法确认是你本人。请重试，或给自己发送一封登录链接邮件。
    
    ## rastrillo.ui.signin_to_keymail
    en: Continue
    ga: Lean ar aghaidh
    bn: চালিয়ে যান
    hi: जारी रखें
    yue: 繼續
    ru: Продолжить
    zh-Hans: 继续
    
    ## rastrillo.ui.signin_continue_heading
    en: Signing you in
    ga: Ag síniú isteach thú
    bn: আপনাকে সাইন ইন করানো হচ্ছে
    hi: आपको साइन इन कराया जा रहा है
    yue: 而家幫你登入
    ru: Выполняем вход
    zh-Hans: 正在为你登录
    
    ## rastrillo.ui.signin_continue_title
    en: Signing in to {name}
    ga: Ag síniú isteach ar {name}
    bn: {name}-এ সাইন ইন করা হচ্ছে
    hi: {name} में साइन इन किया जा रहा है
    yue: 而家登入 {name}
    ru: Входим в {name}
    zh-Hans: 正在登录 {name}
    
    ## rastrillo.ui.signin_continue_body
    en: Taking you to confirm it's you.
    ga: Ag tabhairt chun a dheimhniú gur tusa atá ann thú.
    bn: এটি আপনিই কিনা নিশ্চিত করতে আপনাকে নিয়ে যাওয়া হচ্ছে।
    hi: यह पुष्टि करने के लिए आपको ले जा रहे हैं कि यह आप ही हैं।
    yue: 而家帶你確認係你本人。
    ru: Переходим к подтверждению, что это вы.
    zh-Hans: 正在带你确认是你本人。
    
    ## rastrillo.ui.signin_way_authenticator
    en: Authenticator app
    ga: Aip fíordheimhnithe
    bn: অথেন্টিকেটর অ্যাপ
    hi: ऑथेंटिकेटर ऐप
    yue: 驗證應用程式
    ru: Приложение-аутентификатор
    zh-Hans: 身份验证器应用
    
    ## rastrillo.ui.signin_way_passphrase
    en: Passphrase
    ga: Pasfhrása
    bn: পাসফ্রেজ
    hi: पासफ़्रेज़
    yue: 密碼短語
    ru: Парольная фраза
    zh-Hans: 密码短语
    
    ## rastrillo.ui.signin_code
    en: Code from your authenticator app
    ga: Cód ó d'aip fíordheimhnithe
    bn: আপনার অথেন্টিকেটর অ্যাপের কোড
    hi: आपके ऑथेंटिकेटर ऐप का कोड
    yue: 驗證應用程式嘅驗證碼
    ru: Код из вашего приложения-аутентификатора
    zh-Hans: 来自身份验证器应用的验证码
    
    ## rastrillo.ui.signin_passphrase
    en: Passphrase
    ga: Pasfhrása
    bn: পাসফ্রেজ
    hi: पासफ़्रेज़
    yue: 密碼短語
    ru: Парольная фраза
    zh-Hans: 密码短语
    
    ## rastrillo.ui.signin_code_submit
    en: Sign in
    ga: Sínigh isteach
    bn: সাইন ইন করুন
    hi: साइन इन करें
    yue: 登入
    ru: Войти
    zh-Hans: 登录
    
    ## rastrillo.ui.signin_problem_code
    en: That code didn't work. Try the latest code, or email yourself a sign-in link.
    ga: Níor oibrigh an cód sin. Bain triail as an gcód is déanaí, nó seol nasc síniúcháin isteach chugat féin trí ríomhphost.
    bn: এই কোডটি কাজ করেনি। সবচেয়ে নতুন কোডটি চেষ্টা করুন, বা নিজেকে একটি সাইন-ইন লিঙ্ক ইমেল করুন।
    hi: यह कोड काम नहीं आया। सबसे नया कोड आज़माएँ, या खुद को साइन-इन लिंक ईमेल करें।
    yue: 個驗證碼唔啱。請試用最新嘅驗證碼，或者寄一條登入連結俾自己。
    ru: Этот код не подошёл. Попробуйте самый свежий код или отправьте себе ссылку для входа.
    zh-Hans: 该验证码无效。请试试最新的验证码，或给自己发送一封登录链接邮件。
    
    ## rastrillo.ui.signin_problem_passphrase
    en: That email and passphrase don't match. Try again, or email yourself a sign-in link.
    ga: Ní hionann an ríomhphost sin agus an phasfhrása. Bain triail eile as, nó seol nasc síniúcháin isteach chugat féin trí ríomhphost.
    bn: ইমেল ও পাসফ্রেজ মিলছে না। আবার চেষ্টা করুন, বা নিজেকে একটি সাইন-ইন লিঙ্ক ইমেল করুন।
    hi: यह ईमेल और पासफ़्रेज़ मेल नहीं खाते। फिर कोशिश करें, या खुद को साइन-इन लिंक ईमेल करें।
    yue: 個電郵同密碼短語唔匹配。請再試一次，或者寄一條登入連結俾自己。
    ru: Эта почта и парольная фраза не совпадают. Попробуйте снова или отправьте себе ссылку для входа.
    zh-Hans: 该电子邮件与密码短语不匹配。请重试，或给自己发送一封登录链接邮件。
    
    ## rastrillo.ui.signin_problem_paused
    en: Too many attempts. Email yourself a sign-in link to sign in now.
    ga: An iomarca iarrachtaí. Seol nasc síniúcháin isteach chugat féin trí ríomhphost chun síniú isteach anois.
    bn: অনেকবার চেষ্টা করা হয়েছে। এখনই সাইন ইন করতে নিজেকে একটি সাইন-ইন লিঙ্ক ইমেল করুন।
    hi: बहुत ज़्यादा कोशिशें हो गईं। अभी साइन इन करने के लिए खुद को साइन-इन लिंक ईमेल करें।
    yue: 試得太多次。請寄一條登入連結俾自己，即刻登入。
    ru: Слишком много попыток. Отправьте себе ссылку для входа, чтобы войти сейчас.
    zh-Hans: 尝试次数过多。给自己发送一封登录链接邮件，即可立即登录。
    
    ## rastrillo.ui.signin_email_instead
    en: Email me a sign-in link instead
    ga: Seol nasc síniúcháin isteach chugam trí ríomhphost ina ionad sin
    bn: বরং আমাকে একটি সাইন-ইন লিঙ্ক ইমেল করুন
    hi: इसके बजाय मुझे साइन-इन लिंक ईमेल करें
    yue: 改為寄條登入連結俾我
    ru: Прислать мне ссылку для входа
    zh-Hans: 改为给我发送登录链接邮件
    
    ## rastrillo.ui.signin_offer_title
    en: Sign in faster next time to {name}
    ga: Sínigh isteach níos tapúla an chéad uair eile ar {name}
    bn: পরের বার {name}-এ আরও দ্রুত সাইন ইন করুন
    hi: अगली बार {name} में और तेज़ी से साइन इन करें
    yue: 下次更快登入 {name}
    ru: Входите быстрее в следующий раз в {name}
    zh-Hans: 下次更快登录 {name}
    
    ## rastrillo.ui.signin_offer_heading
    en: Sign in faster next time
    ga: Sínigh isteach níos tapúla an chéad uair eile
    bn: পরের বার আরও দ্রুত সাইন ইন করুন
    hi: अगली बार और तेज़ी से साइन इन करें
    yue: 下次更快登入
    ru: Входите быстрее в следующий раз
    zh-Hans: 下次更快登录
    
    ## rastrillo.ui.signin_offer_body
    en: Add another way to sign in so you don't need an email each time.
    ga: Cuir bealach eile isteach leis chun síniú isteach, ionas nach mbeidh ríomhphost uait gach uair.
    bn: সাইন ইন করার আরেকটি উপায় যোগ করুন, যাতে প্রতিবার ইমেলের দরকার না হয়।
    hi: साइन इन करने का एक और तरीका जोड़ें, ताकि हर बार ईमेल की ज़रूरत न पड़े।
    yue: 加多一種登入方法，咁就唔使次次都收電郵。
    ru: Добавьте ещё один способ входа, чтобы не запрашивать письмо каждый раз.
    zh-Hans: 添加另一种登录方式，这样就不必每次都收邮件。
    
    ## rastrillo.ui.signin_offer_recommended
    en: Recommended
    ga: Molta
    bn: প্রস্তাবিত
    hi: सुझाया गया
    yue: 建議
    ru: Рекомендуем
    zh-Hans: 推荐
    
    ## rastrillo.ui.signin_offer_passkey
    en: Use a passkey
    ga: Úsáid eochair rochtana
    bn: পাসকি ব্যবহার করুন
    hi: पासकी इस्तेमाल करें
    yue: 使用通行密鑰
    ru: Использовать ключ доступа
    zh-Hans: 使用通行密钥
    
    ## rastrillo.ui.signin_offer_passkey_detail
    en: Use your face, fingerprint, or screen lock.
    ga: Úsáid d'aghaidh, do mhéarlorg nó do ghlas scáileáin.
    bn: আপনার মুখ, আঙুলের ছাপ বা স্ক্রিন লক ব্যবহার করুন।
    hi: अपने चेहरे, फ़िंगरप्रिंट या स्क्रीन लॉक का इस्तेमाल करें।
    yue: 用你嘅面容、指紋或者螢幕鎖。
    ru: Ваше лицо, отпечаток пальца или блокировка экрана.
    zh-Hans: 使用你的面容、指纹或屏幕锁定。
    
    ## rastrillo.ui.signin_offer_authenticator
    en: Use an authenticator app
    ga: Úsáid aip fíordheimhnithe
    bn: অথেন্টিকেটর অ্যাপ ব্যবহার করুন
    hi: ऑथेंटिकेटर ऐप इस्तेमाल करें
    yue: 使用驗證應用程式
    ru: Использовать приложение-аутентификатор
    zh-Hans: 使用身份验证器应用
    
    ## rastrillo.ui.signin_offer_authenticator_detail
    en: A six-digit code from an authenticator app.
    ga: Cód sé dhigit ó aip fíordheimhnithe.
    bn: অথেন্টিকেটর অ্যাপ থেকে পাওয়া ছয় অঙ্কের একটি কোড।
    hi: ऑथेंटिकेटर ऐप से मिला छह अंकों का कोड।
    yue: 驗證應用程式顯示嘅六位數字驗證碼。
    ru: Шестизначный код из приложения на вашем телефоне.
    zh-Hans: 来自手机应用的六位验证码。
    
    ## rastrillo.ui.signin_offer_passphrase
    en: Set a passphrase
    ga: Socraigh pasfhrása
    bn: পাসফ্রেজ সেট করুন
    hi: पासफ़्रेज़ सेट करें
    yue: 設定密碼短語
    ru: Задать парольную фразу
    zh-Hans: 设置密码短语
    
    ## rastrillo.ui.signin_offer_passphrase_detail
    en: A long phrase only you know.
    ga: Frása fada nach bhfuil ar eolas ach agatsa.
    bn: একটি লম্বা বাক্যাংশ, যা শুধু আপনি জানেন।
    hi: एक लंबा वाक्यांश, जो सिर्फ़ आप जानते हैं।
    yue: 一段淨係你知道嘅長短語。
    ru: Длинная фраза, которую знаете только вы.
    zh-Hans: 只有你知道的一长串短语。
    
    ## rastrillo.ui.signin_offer_later
    en: Not now
    ga: Ní anois
    bn: এখন নয়
    hi: अभी नहीं
    yue: 暫時唔使
    ru: Не сейчас
    zh-Hans: 暂不
    
    ## rastrillo.ui.signin_add_passkey_heading
    en: Add a passkey
    ga: Cuir eochair rochtana leis
    bn: পাসকি যোগ করুন
    hi: पासकी जोड़ें
    yue: 加入通行密鑰
    ru: Добавить ключ доступа
    zh-Hans: 添加通行密钥
    
    ## rastrillo.ui.signin_add_passkey_body
    en: Your device will ask for your face, fingerprint, or screen lock.
    ga: Iarrfaidh do ghléas d'aghaidh, do mhéarlorg nó do ghlas scáileáin.
    bn: আপনার ডিভাইস আপনার মুখ, আঙুলের ছাপ বা স্ক্রিন লক চাইবে।
    hi: आपका डिवाइस आपके चेहरे, फ़िंगरप्रिंट या स्क्रीन लॉक के लिए कहेगा।
    yue: 你部裝置會要求你提供面容、指紋或者螢幕鎖。
    ru: Устройство попросит ваше лицо, отпечаток пальца или блокировку экрана.
    zh-Hans: 你的设备会要求验证面容、指纹或屏幕锁定。
    
    ## rastrillo.ui.signin_add_passkey_submit
    en: Create a passkey
    ga: Cruthaigh eochair rochtana
    bn: পাসকি তৈরি করুন
    hi: पासकी बनाएँ
    yue: 建立通行密鑰
    ru: Создать ключ доступа
    zh-Hans: 创建通行密钥
    
    ## rastrillo.ui.signin_add_passkey_failed
    en: That didn't work. Try again, or choose another way to sign in.
    ga: Níor oibrigh sé sin. Bain triail eile as, nó roghnaigh bealach eile chun síniú isteach.
    bn: এটি কাজ করেনি। আবার চেষ্টা করুন, বা সাইন ইন করার অন্য একটি উপায় বেছে নিন।
    hi: यह नहीं हो पाया। फिर कोशिश करें, या साइन इन करने का कोई दूसरा तरीका चुनें।
    yue: 唔成功。請再試一次，或者揀其他登入方法。
    ru: Не получилось. Попробуйте снова или выберите другой способ входа.
    zh-Hans: 未能成功。请重试，或选择其他登录方式。
    
    ## rastrillo.ui.signin_add_authenticator_heading
    en: Add an authenticator app
    ga: Cuir aip fíordheimhnithe leis
    bn: অথেন্টিকেটর অ্যাপ যোগ করুন
    hi: ऑथेंटिकेटर ऐप जोड़ें
    yue: 加入驗證應用程式
    ru: Добавить приложение-аутентификатор
    zh-Hans: 添加身份验证器应用
    
    ## rastrillo.ui.signin_add_authenticator_body
    en: Scan the code with your authenticator app, then type the code it shows.
    ga: Scan an cód le d'aip fíordheimhnithe, ansin clóscríobh an cód a thaispeánann sí.
    bn: আপনার অথেন্টিকেটর অ্যাপ দিয়ে কোডটি স্ক্যান করুন, তারপর অ্যাপ যে কোড দেখায় তা লিখুন।
    hi: अपने ऑथेंटिकेटर ऐप से कोड स्कैन करें, फिर ऐप जो कोड दिखाए उसे लिखें।
    yue: 用驗證應用程式掃描呢個二維碼，然後輸入佢顯示嘅驗證碼。
    ru: Отсканируйте код в приложении-аутентификаторе и введите код, который оно покажет.
    zh-Hans: 用身份验证器应用扫描此二维码，然后输入它显示的验证码。
    
    ## rastrillo.ui.signin_add_authenticator_key
    en: Can't scan the code? Type this key instead:
    ga: Ní féidir leat é a scanadh? Clóscríobh an eochair seo ina ionad:
    bn: স্ক্যান করতে পারছেন না? বরং এই কী-টি লিখুন:
    hi: स्कैन नहीं कर पा रहे? इसके बजाय यह कुंजी लिखें:
    yue: 掃描唔到？請改為輸入呢個密鑰：
    ru: Не получается отсканировать? Введите вместо этого этот ключ:
    zh-Hans: 无法扫描？请改为输入此密钥：
    
    ## rastrillo.ui.signin_add_authenticator_submit
    en: Add authenticator app
    ga: Cuir aip fíordheimhnithe leis
    bn: অথেন্টিকেটর অ্যাপ যোগ করুন
    hi: ऑथेंटिकेटर ऐप जोड़ें
    yue: 加入驗證應用程式
    ru: Добавить приложение
    zh-Hans: 添加身份验证器应用
    
    ## rastrillo.ui.signin_add_authenticator_wrong
    en: That code didn't match. Try the latest code from your app.
    ga: Níor mheaitseáil an cód sin. Bain triail as an gcód is déanaí a thaispeánann d'aip.
    bn: কোডটি মেলেনি। অ্যাপে দেখানো সবচেয়ে নতুন কোডটি লিখুন।
    hi: यह कोड मेल नहीं खाया। ऐप में दिखा सबसे नया कोड लिखें।
    yue: 驗證碼唔匹配。請輸入應用程式顯示嘅最新驗證碼。
    ru: Этот код не подошёл. Введите самый свежий код из приложения.
    zh-Hans: 验证码不匹配。请输入应用显示的最新验证码。
    
    ## rastrillo.ui.signin_add_passphrase_heading
    en: Set a passphrase
    ga: Socraigh pasfhrása
    bn: পাসফ্রেজ সেট করুন
    hi: पासफ़्रेज़ सेट करें
    yue: 設定密碼短語
    ru: Задать парольную фразу
    zh-Hans: 设置密码短语
    
    ## rastrillo.ui.signin_add_passphrase_body
    en: Use at least 15 characters. A few unrelated words work well.
    ga: Úsáid 15 charachtar ar a laghad. Oibríonn cúpla focal nach bhfuil baint acu lena chéile go maith.
    bn: কমপক্ষে ১৫টি অক্ষর ব্যবহার করুন। কয়েকটি অসম্পর্কিত শব্দ ভালো কাজ করে।
    hi: कम से कम 15 अक्षर रखें। कुछ असंबंधित शब्द अच्छा काम करते हैं।
    yue: 請用最少 15 個字元。幾個互不相關嘅詞語就好好用。
    ru: Используйте не менее 15 символов. Хорошо подходит несколько не связанных между собой слов.
    zh-Hans: 至少使用 15 个字符。几个互不相关的词就很好用。
    
    ## rastrillo.ui.signin_add_passphrase_short
    en: Use at least 15 characters.
    ga: Úsáid 15 charachtar ar a laghad.
    bn: কমপক্ষে ১৫টি অক্ষর ব্যবহার করুন।
    hi: कम से कम 15 अक्षर रखें।
    yue: 請用最少 15 個字元。
    ru: Используйте не менее 15 символов.
    zh-Hans: 至少使用 15 个字符。
    
    ## rastrillo.ui.signin_add_passphrase_long
    en: Use 256 characters or fewer.
    ga: Úsáid 256 carachtar nó níos lú.
    bn: সর্বোচ্চ ২৫৬টি অক্ষর ব্যবহার করুন।
    hi: ज़्यादा से ज़्यादा 256 अक्षर रखें।
    yue: 請用最多 256 個字元。
    ru: Используйте не более 256 символов.
    zh-Hans: 最多使用 256 个字符。
    
    ## rastrillo.ui.signin_add_passphrase_submit
    en: Save passphrase
    ga: Sábháil an phasfhrása
    bn: পাসফ্রেজ সংরক্ষণ করুন
    hi: पासफ़्रेज़ सहेजें
    yue: 儲存密碼短語
    ru: Сохранить парольную фразу
    zh-Hans: 保存密码短语
    
    ## rastrillo.ui.signin_add_passphrase_show
    en: Show passphrase
    ga: Taispeáin an pasfhrása
    bn: পাসফ্রেজ দেখান
    hi: पासफ़्रेज़ दिखाएँ
    yue: 顯示密碼短語
    ru: Показать парольную фразу
    zh-Hans: 显示密码短语
    
    ## rastrillo.ui.signin_added
    en: Done. Next time you can sign in with it.
    ga: Déanta. An chéad uair eile, beidh tú in ann síniú isteach léi.
    bn: হয়ে গেছে। পরের বার আপনি এটি দিয়ে সাইন ইন করতে পারবেন।
    hi: हो गया। अगली बार आप इससे साइन इन कर सकेंगे।
    yue: 搞掂。下次你可以用佢登入。
    ru: Готово. В следующий раз вы сможете войти с её помощью.
    zh-Hans: 完成。下次你可以用它登录。
    
    ## rastrillo.ui.signin_added_continue
    en: Continue
    ga: Lean ar aghaidh
    bn: চালিয়ে যান
    hi: जारी रखें
    yue: 繼續
    ru: Продолжить
    zh-Hans: 继续
    
    ## rastrillo.ui.signin_options_title
    en: Sign-in options for {name}
    ga: Roghanna síniúcháin isteach do {name}
    bn: {name}-এর সাইন-ইন বিকল্প
    hi: {name} के साइन-इन विकल्प
    yue: {name} 嘅登入選項
    ru: Варианты входа в {name}
    zh-Hans: {name} 的登录选项
    
    ## rastrillo.ui.signin_options_heading
    en: Sign-in options
    ga: Roghanna síniúcháin isteach
    bn: সাইন-ইন বিকল্প
    hi: साइन-इन विकल्प
    yue: 登入選項
    ru: Варианты входа
    zh-Hans: 登录选项
    
    ## rastrillo.ui.signin_options_email
    en: Email link
    ga: Nasc ríomhphoist
    bn: ইমেল লিঙ্ক
    hi: ईमेल लिंक
    yue: 電郵連結
    ru: Ссылка по почте
    zh-Hans: 邮件链接
    
    ## rastrillo.ui.signin_options_email_detail
    en: Always on. We email a link to {address}.
    ga: Ar siúl i gcónaí. Seolaimid nasc chuig {address}.
    bn: সবসময় চালু। আমরা {address}-এ একটি লিঙ্ক ইমেল করি।
    hi: हमेशा चालू। हम {address} पर लिंक ईमेल करते हैं।
    yue: 一直開住。我哋會寄條連結去 {address}。
    ru: Всегда включено. Мы отправляем ссылку на {address}.
    zh-Hans: 始终开启。我们会向 {address} 发送链接。
    
    ## rastrillo.ui.signin_options_add
    en: Add
    ga: Cuir leis
    bn: যোগ করুন
    hi: जोड़ें
    yue: 加入
    ru: Добавить
    zh-Hans: 添加
    
    ## rastrillo.ui.signin_options_remove
    en: Remove
    ga: Bain
    bn: সরান
    hi: हटाएँ
    yue: 移除
    ru: Удалить
    zh-Hans: 移除
    
    ## rastrillo.ui.signin_options_removed
    en: Removed.
    ga: Baineadh.
    bn: সরানো হয়েছে।
    hi: हटा दिया गया।
    yue: 已移除。
    ru: Удалено.
    zh-Hans: 已移除。
    
    ## rastrillo.ui.signin_add_authenticator_qr
    en: QR code for your authenticator app
    ga: Cód QR do d'aip fíordheimhnithe
    bn: আপনার অথেন্টিকেটর অ্যাপের জন্য QR কোড
    hi: आपके ऑथेंटिकेटर ऐप के लिए QR कोड
    yue: 俾驗證應用程式用嘅二維碼
    ru: QR-код для приложения-аутентификатора
    zh-Hans: 用于身份验证器应用的二维码

### Astra's whole reply

    ga rastrillo.ui.signin_problem_keymail: Níorbh fhéidir linn a dheimhniú gur tusa atá ann. Bain triail eile as, nó seol nasc sínithe isteach chugat féin trí ríomhphost. — “Síniúchán” refers to a signature or autograph; use the sign-in term “síniú isteach” ([https://www.focloir.ie/en/dictionary/nid/síniú](https://www.focloir.ie/en/dictionary/nid/síniú)).
    ga rastrillo.ui.signin_problem_code: Níor oibrigh an cód sin. Bain triail as an gcód is déanaí, nó seol nasc sínithe isteach chugat féin trí ríomhphost. — Corrects the term for “sign-in link.”
    ga rastrillo.ui.signin_problem_passphrase: Ní thagann an seoladh ríomhphoist sin leis an bpasfhrása. Bain triail eile as, nó seol nasc sínithe isteach chugat féin trí ríomhphost. — The credentials do not match; the original says they are not identical. Also corrects “sign-in link.”
    ga rastrillo.ui.signin_problem_paused: An iomarca iarrachtaí. Seol nasc sínithe isteach chugat féin trí ríomhphost chun síniú isteach anois. — Corrects the term for “sign-in link.”
    ga rastrillo.ui.signin_email_instead: Seol nasc sínithe isteach chugam trí ríomhphost ina ionad sin — Corrects the term for “sign-in link.”
    ga rastrillo.ui.signin_added: Déanta. An chéad uair eile, beidh tú in ann síniú isteach ar an mbealach seo. — The feminine pronoun “léi” does not cover every sign-in method.
    ga rastrillo.ui.signin_options_title: Roghanna sínithe isteach do {name} — Uses the correct sign-in term.
    ga rastrillo.ui.signin_options_heading: Roghanna sínithe isteach — Uses the correct sign-in term.
    ga rastrillo.ui.signin_options_email_detail: Ar siúl i gcónaí. Seolaimid nasc trí ríomhphost chuig {address}. — Restores the explicit email delivery method.
    bn: no corrections
    hi: no corrections
    yue rastrillo.ui.signin_problem_keymail: 我哋確認唔到係你本人。請再試一次，或者用電郵寄一條登入連結俾自己。 — Restores the explicit email delivery method.
    yue rastrillo.ui.signin_problem_code: 個驗證碼唔啱。請試用最新嘅驗證碼，或者用電郵寄一條登入連結俾自己。 — Restores the explicit email delivery method.
    yue rastrillo.ui.signin_problem_passphrase: 個電郵同密碼短語唔匹配。請再試一次，或者用電郵寄一條登入連結俾自己。 — Restores the explicit email delivery method.
    yue rastrillo.ui.signin_problem_paused: 試得太多次。請用電郵寄一條登入連結俾自己，即刻登入。 — Restores the explicit email delivery method.
    yue rastrillo.ui.signin_email_instead: 改為用電郵寄條登入連結俾我 — Restores the explicit email delivery method.
    yue rastrillo.ui.signin_options_email_detail: 一直開住。我哋會用電郵寄條連結去 {address}。 — Restores the explicit email delivery method.
    ru rastrillo.ui.signin_problem_keymail: Нам не удалось подтвердить, что это вы. Попробуйте снова или отправьте себе ссылку для входа по электронной почте. — Restores the explicit email delivery method.
    ru rastrillo.ui.signin_problem_code: Этот код не подошёл. Попробуйте самый свежий код или отправьте себе ссылку для входа по электронной почте. — Restores the explicit email delivery method.
    ru rastrillo.ui.signin_problem_passphrase: Эта почта и парольная фраза не совпадают. Попробуйте снова или отправьте себе ссылку для входа по электронной почте. — Restores the explicit email delivery method.
    ru rastrillo.ui.signin_problem_paused: Слишком много попыток. Отправьте себе ссылку для входа по электронной почте, чтобы войти сейчас. — Restores the explicit email delivery method.
    ru rastrillo.ui.signin_email_instead: Вместо этого прислать мне ссылку для входа по электронной почте — Restores “instead” and the email delivery method.
    ru rastrillo.ui.signin_offer_authenticator_detail: Шестизначный код из приложения-аутентификатора. — Removes the unsupported phone restriction and restores “authenticator app.”
    ru rastrillo.ui.signin_add_authenticator_submit: Добавить приложение-аутентификатор — Restores the specific app type, consistent with the other strings.
    ru rastrillo.ui.signin_added: Готово. В следующий раз вы сможете войти с помощью этого способа. — The feminine pronoun “её” only fits the passphrase, not the other sign-in methods.
    ru rastrillo.ui.signin_options_email_detail: Всегда включено. Мы отправляем ссылку по электронной почте на {address}. — Restores the explicit email delivery method.
    zh-Hans rastrillo.ui.signin_offer_authenticator_detail: 来自身份验证器应用的六位验证码。 — Removes the unsupported phone restriction and restores “authenticator app.”
    zh-Hans rastrillo.ui.signin_offer_passphrase_detail: 只有你知道的一个长短语。 — The original describes a string of phrases rather than one long phrase.
    zh-Hans rastrillo.ui.signin_options_email_detail: 始终开启。我们会通过电子邮件向 {address} 发送链接。 — Restores the explicit email delivery method.
### Applied

All 27 corrections, verbatim; bn and hi had none.

- ru and zh-Hans `signin_offer_authenticator_detail` no longer say the
  code comes from a phone, which the English stopped saying.
- ga uses `nasc sínithe isteach` and `Roghanna sínithe isteach`, the
  form its older sign-in strings already use, in place of
  `síniúcháin` (a signature). Its passphrase error said the email and
  passphrase "are not the same"; it now says they do not match.
- ga and ru `signin_added` no longer use a feminine pronoun that fits
  the passphrase alone.
- ru `signin_add_authenticator_submit` names the authenticator app, and
  `signin_email_instead` keeps "instead".
- ru and yue say the sign-in link comes by email, as the English does;
  ga, ru, yue and zh-Hans `signin_options_email_detail` say the link is
  emailed.
- zh-Hans `signin_offer_passphrase_detail` says one long phrase, not a
  long string of phrases.
