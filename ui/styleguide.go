package ui

// styleguideSamples are the canonical markup samples for the class
// idioms — structural components with arbitrary bodies that a Go
// template partial cannot wrap. Styleguide returns them; the
// design-system page renders every one so each documented class is
// exercised, and ui_test.go's TestIdiomClassesAreStyled keeps them
// honest against tokens.css (the F10 lesson, generalized). ui.go's
// package doc references Styleguide by name rather than duplicating the
// markup, so the two cannot drift.
//
// Icon markup below is the vendored Lucide SVG (icons.go's "kebab",
// "chevron-down" and "help-circle") inlined literally rather than built
// with {{icon "..."}}: these samples are plain HTML strings with no
// template actions, so there is nothing to execute a template func
// against.
var styleguideSamples = map[string]string{
	"box": `<div class="rst-box-head"><h2>Payout</h2><a class="rst-btn" href="/payout/edit">Edit</a></div>
<section class="rst-box"><p>Everything on a screen sits inside boxes.</p><div class="rst-box-foot">Last updated 2 hours ago</div></section>`,
	"list-grid": `<div class="rst-card" style="--rst-cols: 2fr 110px 32px">
  <div class="rst-lrow rst-lrow--head"><span>Order</span><span class="rst-m-hide">Status</span><span></span></div>
  <div class="rst-lrow">
    <a class="rst-nm" href="/orders/AB3PX">Grace Hopper<small>AB3PX · grace@example.com</small></a>
    <span class="rst-m-hide rst-cell-mut">Paid</span>
    <details class="rst-row-menu"><summary aria-label="Actions for order AB3PX"><svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="1"/><circle cx="12" cy="5" r="1"/><circle cx="12" cy="19" r="1"/></svg></summary>
      <div class="rst-row-menu__panel"><a href="/orders/AB3PX">View</a><hr><button type="submit" class="rst-danger">Refund order…</button></div>
    </details>
  </div>
  <p class="rst-no-match">No orders match. <a href="/orders">Clear filters</a></p>
</div>
<p class="rst-count-line">Displaying <strong>1–20</strong> of <strong>412</strong></p>`,
	"dropdown": `<details class="rst-dropdown" name="list-controls">
  <summary>Filter<span class="rst-caret" aria-hidden="true"><svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg></span><span class="rst-sr-only">Filter orders: Paid</span></summary>
  <div class="rst-dropdown__menu">
    <a aria-current="true" href="/orders?status=paid">Paid</a>
    <details class="rst-menu-group" open><summary>Price</summary><div><a href="/orders?price=free">Free</a></div></details>
  </div>
</details>
<span class="rst-ftok"><span class="rst-ftok__k">Paid</span><a href="/orders" aria-label="Remove filter Paid">✕</a></span>`,
	// form-layout demonstrates the classes tokens.css ships for form
	// rhythm and the save bar (rst-form-flow, rst-field-row, rst-grow,
	// rst-form-foot, rst-form-actions) — no partial emits these, since
	// they wrap a caller-composed run of "field" partials rather than a
	// single data shape. Two adjacent .rst-field divs exercise the
	// rst-form-flow spacing rule; the row's grown field exercises
	// rst-grow. The cancel/save pair reuses the existing button classes
	// (Task 3's ambiguity resolution: no new rst-btn variant needed).
	"form-layout": `<form class="rst-form-flow" method="post" action="/settings">
  <div class="rst-field">
    <label class="rst-field__label" for="name">Name</label>
    <input class="rst-input" type="text" id="name" name="name">
  </div>
  <div class="rst-field">
    <label class="rst-field__label" for="email">Email</label>
    <input class="rst-input" type="email" id="email" name="email">
  </div>
  <div class="rst-field-row">
    <div class="rst-field rst-grow">
      <label class="rst-field__label" for="city">City</label>
      <input class="rst-input" type="text" id="city" name="city">
    </div>
    <div class="rst-field">
      <label class="rst-field__label" for="zip">ZIP</label>
      <input class="rst-input rst-input--short" type="text" id="zip" name="zip">
    </div>
  </div>
  <div class="rst-form-foot">
    <span class="rst-form-foot__note">Changes save immediately.</span>
    <div class="rst-form-actions">
      <a class="rst-btn" href="/settings">Cancel</a>
      <button class="rst-btn rst-btn--primary" type="submit">Save</button>
    </div>
  </div>
</form>`,
	// tblock reuses field-check's exact switch markup (input + a sibling
	// rst-switch__track) inside its own head, so :has() can key off the
	// same input:checked selector tokens.css already ships for the
	// switch. The body is hand-written static HTML — a caller's real
	// body would be a "field" partial render, but this sample has no
	// template engine of its own supplying that, so a plain input
	// stands in for it.
	"tblock": `<div class="rst-tblock">
  <label class="rst-tblock__head"><input type="checkbox" name="notify" checked>
    <span class="rst-switch__track" aria-hidden="true"></span>
    <span><span class="rst-tblock__title">Email notifications</span><span class="rst-tblock__desc">Sent for every reply to a thread you're in.</span></span>
  </label>
  <div class="rst-tblock__body">
    <div class="rst-field">
      <label class="rst-field__label" for="notify-freq">Frequency</label>
      <input class="rst-input" type="text" id="notify-freq" name="notify_freq" value="Daily digest">
    </div>
  </div>
</div>`,
	// modal route — the backdrop is marked inert (a real HTML attribute,
	// not a class tokens.css needs to style) so the page behind the
	// panel is unreachable by keyboard or screen reader while the modal
	// is open. The nav rail's current item is aria-current, matching the
	// dropdown and seg-tabs idioms. Closing is the plain rst-modal-close
	// link back to the page the backdrop already shows.
	"modal": `<div class="rst-backdrop" inert>
  <div class="rst-page"><h1>Settings</h1></div>
</div>
<div class="rst-modal-overlay">
  <div class="rst-modal-panel">
    <nav>
      <a href="/settings/profile" aria-current="page">Profile</a>
      <a href="/settings/billing">Billing</a>
      <a href="/settings/notifications">Notifications</a>
    </nav>
    <section>
      <a class="rst-modal-close" href="/settings" aria-label="Close settings">✕</a>
      <h2>Profile</h2>
      <p>Update the name and photo shown across the account.</p>
    </section>
  </div>
</div>`,
	// help — the CSS tooltip (data-tip, shown via rst-tip::after on
	// hover/focus) is decoration only; aria-label carries the real
	// accessible name so a screen reader user gets the full sentence
	// even though the tooltip itself never reaches the accessibility
	// tree.
	"help": `<a class="rst-help rst-tip" href="/help/orders" target="_blank" rel="noopener" aria-label="Help: orders" data-tip="About orders"><svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/></svg></a>`,
	// selbox — the label restates the row's own identity ("order
	// AB3PX"), the same disambiguation list-row-action's ActionAria and
	// row-menu's per-row aria-label already use, rather than a bare
	// "checkbox 3 of 12".
	"selbox": `<label class="rst-selbox"><input type="checkbox" aria-label="Select order AB3PX"></label>`,
	// shell-topbar — one of the two page frames a shell puts around
	// .rst-page: a skip link first in the DOM, a bar carrying brand, nav
	// and an account dropdown pushed to the inline end, then the page
	// column and a footer. No partial emits
	// any of this — an app's layout template owns its own shell — so
	// this sample is the only exercise these classes get. The nav's
	// current item is aria-current, the same signal the dropdown and
	// seg-tabs idioms already use.
	"shell-topbar": `<div class="rst-shell-topbar">
  <a class="rst-skip" href="#main">Skip to content</a>
  <header class="rst-shell__bar"><a class="rst-shell__brand" href="/">Notes</a>
    <nav class="rst-shell__nav"><a href="/" aria-current="page">Home</a><a href="/archive">Archive</a></nav>
    <details class="rst-dropdown rst-shell__account"><summary>Account<span class="rst-caret" aria-hidden="true"><svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg></span></summary>
      <div class="rst-dropdown__menu"><a href="/settings">Settings</a></div></details>
  </header>
  <main class="rst-page" id="main">Content.</main>
  <footer class="rst-shell__foot">Made with rastrillo</footer>
</div>`,
	// shell-sidebar — the same frame with a rail instead of a bar. The
	// narrow-screen disclosure is a native <details> strip whose open
	// state reveals the rail (the adjacent-sibling selector in
	// tokens.css), so the shell stays zero-JS like every other idiom
	// here. The rail's own .rst-page still wraps the content, so a
	// screen's markup is identical in either shell.
	"shell-sidebar": `<div class="rst-shell-sidebar">
  <a class="rst-skip" href="#main">Skip to content</a>
  <details class="rst-shell__chrome"><summary>Menu</summary></details>
  <aside class="rst-shell__rail"><a class="rst-shell__brand" href="/">Notes</a>
    <nav class="rst-shell__nav"><span class="rst-shell__group">Work</span><a href="/" aria-current="page">Dashboard</a><a href="/reports">Reports</a></nav>
  </aside>
  <main class="rst-shell__main" id="main"><div class="rst-page">Content.</div></main>
</div>`,
}

// Styleguide returns the canonical markup for every class idiom —
// structural components with arbitrary bodies that a template partial
// cannot wrap, such as the section box, the list-grid card and the page
// shells. It is the source the design-system page renders and the
// source ui_test.go's TestStyleguideSamplesRender and
// TestIdiomClassesAreStyled hold honest against tokens.css: a sample
// cannot use a class tokens.css doesn't style, and every idiom class
// tokens.css ships must appear in some sample.
//
// The returned map is a copy; mutating it does not affect future calls.
func Styleguide() map[string]string {
	out := make(map[string]string, len(styleguideSamples))
	for k, v := range styleguideSamples {
		out[k] = v
	}
	return out
}
