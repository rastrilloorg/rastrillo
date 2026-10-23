# ui partials

Generated from ui/partials by TestSkillPartialsReferenceIsCurrent; do not edit.
Call one as `{{template "name" dict "L" $L "Key" value ...}}`; `L` is the localizer every page starts with. A key `X[]` is a list whose items carry the keys in brackets. Keys a Go helper reads for the template (row-menu's and pagination's items, say) are not listed; each template's own comment says more.

- **actor**: Who did something, as words: nothing for the app, the person's name, or "Claude for Paul" for an agent acting for them. Keys: `Actor`, `Display`.
- **api-tokens**: The API token page's body, from apitoken's PageData: the one-time reveal, the create form, the list, and the revoke-all buttons. Keys: `Page`, `Path`, `RevokeAgentPath`, `RevokeAllPath`, `RevokePath`.
- **back-nav**: The ← return link above a confirm or detail page. Keys: `Href`, `Label`.
- **badge**: An uppercase bordered chip for small static markers (LIVE, Draft, Sold out): distinct from status-pill, which is record status. Keys: `Label`, `Tone`.
- **bulk-bar**: Select-mode's header strip, replacing the list-bar: count, escalate/clear link, and the actions menu whose items are real submit buttons on the surrounding form. Keys: `Actions[]` (Danger, Label, Value), `Count`, `DoneHref`, `DoneLabel`, `EscalateHref`, `EscalateLabel`, `MenuLabel`.
- **callout**: The one alert vocabulary: tinted bordered strip, tone icon, optional bold title, one body paragraph. Keys: `Alert`, `Body`, `Focus`, `ID`, `Title`, `Tone`.
- **choice-field**: Option cards, the preferred radio/checkbox group: whole-card hit target, native input visible (it signals pick-one vs pick-any), selection ring via :has(). Keys: `LegendHidden`, `Legend`, `Name`, `Options[]` (Checked, Desc, Title, Value), `Type`.
- **confirm-form**: The destructive-action POST, plus its page companions. Keys: `Action`, `CancelHref`, `CancelLabel`, `Danger`, `Hidden[]`, `Label`.
- **detail-list**: A record's labelled facts as a definition list — the Show state's body. Keys: `Items[]` (DateTime, Label, Mono, Value).
- **dropdown**: A zero-JS disclosure: a summary button that opens a panel of plain links. Keys: `Aria`, `Items[]` (Current, Href, Label), `Label`.
- **empty-state**: The blank-state card: a heading, one specific sentence, and at most one call to action. Keys: `ActionHref`, `ActionLabel`, `Body`, `Hidden[]`, `PostAction`, `Title`.
- **error-page**: The whole body of an error response: the status, one honest sentence about what happened, a way back to somewhere real, and (for a 500) the reference an operator can grep for. Keys: `BackHref`, `Body`, `HomeHref`, `Ref`, `Status`, `Title`.
- **field**: Label + input + optional hint/help/error, the one text-like control. Keys: `Autocomplete`, `Autofocus`, `Error`, `Help`, `Hint`, `ID`, `Inputmode`, `Label`, `Max`, `Maxlength`, `Min`, `Name`, `Pattern`, `Placeholder`, `Primary`, `QuietError`, `Required`, `Short`, `Type`, `Value`.
- **field-check**: The one toggle mechanism: a real checkbox skinned as a switch. Keys: `Checked`, `Disabled`, `Label`, `Name`, `Value`.
- **field-date**: One labelled date input. Keys: `Error`, `Hint`, `Label`, `Max`, `Min`, `Name`, `Plain`, `Required`, `Value`.
- **field-daterange**: A start and an end as one labelled group: a fieldset for the pair, two of the singular date partials inside a rst-field-row, and the data-rst-range marker that tells ui/datetime.js the two inputs belong together. Keys: `End`, `Kind`, `LegendHidden`, `Legend`, `Seed`, `Start`.
- **field-datetime**: One labelled date-and-time input. Keys: `Error`, `Hint`, `Label`, `Max`, `Min`, `Name`, `Plain`, `Required`, `Value`.
- **field-money**: One labelled money input in one currency, with optional hint and error lines wired as field-text wires them. Keys: `Currency`, `Error`, `Hint`, `Label`, `Locale`, `Name`, `Required`, `Seed`, `Value`.
- **field-select**: Field's envelope (label/hint/help/error, identical describedby wiring) around a native <select>. Keys: `Error`, `Help`, `Hint`, `ID`, `Label`, `Name`, `Options[]` (Label, Selected, Value), `Plain`, `Required`, `Short`.
- **field-text**: One labelled text input with optional hint and error lines, wired together for assistive tech: the hint and error get ids derived from Name, aria-describedby lists exactly the ids that render, and an Error also sets aria-invalid. Keys: `Autocomplete`, `Autofocus`, `Error`, `Hint`, `Label`, `Minlength`, `Name`, `Primary`, `Required`, `Type`, `Value`.
- **field-textarea**: Field-text's wrapper around a <textarea>. Keys: `Error`, `Hint`, `Label`, `Name`, `Required`, `Rows`, `Value`.
- **field-time**: One labelled time input. Keys: `Error`, `Hint`, `Label`, `Max`, `Min`, `Name`, `Plain`, `Required`, `Value`.
- **field-url**: One labelled web-address input that does not care whether the scheme was typed. Keys: `Autocomplete`, `Error`, `Hint`, `Label`, `Name`, `Required`, `Value`.
- **form-error**: A one-line validation failure; renders nothing when empty. Takes a value, not a dict: `{{template "form-error" .Message}}`.
- **form-foot**: A form's closing action row: one primary submit and an optional cancel link. Keys: `CancelHref`, `CancelLabel`, `Proof`, `Submit`.
- **job-status**: A background job's polled fragment: spinner and progress while running, the outcome once it isn't. Keys: `Err`, `Name`, `Percent`, `PollSeconds`, `PollURL`, `Progress`, `PushURL`, `Status`.
- **list-bar**: The toolbar strip at the top of a list card: the search form, and an optional filter dropdown in the space to its right (the search field is capped at 20rem in tokens.css precisely so that room stays free). Keys: `Filter`, `Hidden`, `Placeholder`, `Query`, `SearchAction`.
- **list-bar-search**: The search form: a magnifying glass, one <input type="search">, any hidden pairs the list needs carried across the GET, and the submit control above. Keys: `Action`, `Hidden[]`, `Placeholder`, `Query`.
- **list-row-action**: One list row: the record's name as the primary link, an optional meta line under it, and one secondary action pill on the right. Keys: `ActionAria`, `ActionHref`, `ActionLabel`, `Href`, `LeadInitial`, `Lead`, `Main`, `Menu`, `StatusLabel`, `StatusTone`, `Sub`.
- **list-search-submit**: The search form's real submit control. Keys: `Label`.
- **locale-menu**: The language switcher (design spec 2026-08-28 §2.4): a details/summary dropdown listing the app's declared locales, each item a one-field POST form to /_locale so the choice is stored in the locale cookie and the user lands on the same path under the new prefix. Keys: `Action`, `Items[]` (Code, Current, Name), `Return`.
- **media-grid**: A grid of image tiles, for an app's Images page: an even 4:3 frame on every tile, the name under it on one line, the description under that on two, and one line of facts the caller formats. Keys: `Items[]` (Alt, Desc, Fit, Height, Href, Menu, Meta, Name, Select, Selected, Src, Width), `SelectName`.
- **meter**: A capacity bar with its number: 412/500 next to a 4px fill. Keys: `Percent`, `Text`.
- **notice**: A one-line calm confirmation; renders nothing when empty. Takes a value, not a dict: `{{template "notice" .Message}}`.
- **page-header**: The screen's title bar: an <h1>, an optional subhead, and at most one primary action. Keys: `ActionHref`, `ActionIcon`, `ActionLabel`, `Sub`, `Title`.
- **pagination**: A numbered page strip of ordinary links, so paging works with JavaScript disabled. Keys: `Label`.
- **person**: Initials avatar + name + email, the identity cell for record types that are people; Large is the Show-header size. Keys: `Email`, `Href`, `Initial`, `Large`, `Name`.
- **row-menu**: A row's secondary actions behind a kebab, in a native <details> in the shared menu group, so opening one closes any other menu, with no script. Keys: `Name`.
- **scheme-menu**: The colour-scheme switch, built the way locale-menu is: a details/summary dropdown of one-field POST forms to /_theme, one each for Light, Dark and System, so the choice lands in the scheme cookie and the reader comes back to the same page. Keys: `Action`, `Return`, `Scheme`.
- **seg-tabs**: A segmented control of server-rendered links: edit-form Basics/Advanced (each tab its own form, so one tab's save can never clobber the other's fields) and list scope-switching. Keys: `Items[]` (Current, Href, Label), `Label`.
- **signin**: The shipped sign-in screen: one card, brand at the start and the way in at the end. Keys: `Brand`, `Preview`, `State`.
- **signin-title**: The document title for the same state, for the page's title block: every state has one heading and a title that says the same thing, so a screen-reader user who lands on the page hears where they are. Keys: `Brand`, `State`.
- **stat**: One reading in a stat band: a label over a number, with an optional change under it. Keys: `Delta`, `Exact`, `Label`, `Lead`, `Note`, `Tone`, `Value`.
- **status-pill**: A status as a tinted pill. Keys: `Label`, `Tone`.
