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
	"box": `<div rst-box-head><h2>Payout</h2><a rst-btn href="/payout/edit">Edit</a></div>
<section rst-box><p>Everything on a screen sits inside boxes.</p><div rst-box-foot>Last updated 2 hours ago</div></section>`,
	"stat-band": `<div rst-stats>
  <div rst-stat="lead"><span rst-stat-label>Revenue this month</span><span rst-stat-num>&euro;48,210</span><span rst-stat-delta rst-tone="positive">+12%</span><span rst-stat-note>vs. last month</span></div>
  <div rst-stat><span rst-stat-label>Orders today</span><span rst-stat-num>318</span></div>
  <div rst-stat><span rst-stat-label>Average basket</span><span rst-stat-num>&euro;41.20</span><span rst-stat-delta rst-tone="negative">&minus;3%</span></div>
  <div rst-stat><span rst-stat-label>Refunds</span><span rst-stat-num>7</span></div>
</div>`,
	"list-grid": `<div rst-card style="--rst-cols: 2fr 110px 32px">
  <div rst-lrow="head"><span>Order</span><span class="rst-m-hide">Status</span><span></span></div>
  <div rst-lrow>
    <a class="rst-nm" href="/orders/AB3PX"><bdi>Grace Hopper</bdi><small>AB3PX · <bdi>grace@example.com</bdi></small></a>
    <span class="rst-m-hide rst-cell-mut">Paid</span>
    <details rst-row-menu name="rst-menus"><summary aria-label="Actions for order AB3PX"><svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="1"/><circle cx="12" cy="5" r="1"/><circle cx="12" cy="19" r="1"/></svg></summary>
      <div rst-row-menu-panel><a href="/orders/AB3PX">View</a><hr><button type="submit" class="rst-danger">Refund order…</button></div>
    </details>
  </div>
  <p rst-no-match>No orders match. <a href="/orders">Clear filters</a></p>
</div>
<p rst-count-line>Displaying <strong>1–20</strong> of <strong>412</strong></p>`,
	// dropdown — "rst-menus" is the shared exclusivity group every
	// dropdown, row-menu and locale menu in the library defaults to, so
	// opening any one of them closes whichever was open. The nested
	// rst-menu-group carries a DIFFERENT name on purpose: <details name>
	// exclusivity is document-wide, not sibling-scoped, so a submenu
	// sharing its parent's group would close the parent the instant it
	// opened — the submenu would flash and vanish. Shell chrome (the
	// sidebar's rst-shell-chrome strip) and the toggle-block stay out of
	// the group entirely: neither is a menu, and closing the sidebar
	// because someone opened a filter would be absurd.
	"dropdown": `<details rst-dropdown name="rst-menus">
  <summary>Filter<span rst-caret aria-hidden="true"><svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg></span><span class="rst-sr-only">Filter orders: Paid</span></summary>
  <div rst-dropdown-menu>
    <a aria-current="true" href="/orders?status=paid">Paid</a>
    <details rst-menu-group name="rst-menus-price" open><summary>Price</summary><div><a href="/orders?price=free">Free</a></div></details>
  </div>
</details>
<span rst-ftok><span rst-ftok-k>Paid</span><a href="/orders" aria-label="Remove filter Paid">✕</a></span>`,
	// form-layout demonstrates the attributes tokens.css ships for form
	// rhythm and the sticky save bar (rst-form-flow, rst-field-row,
	// rst-grow, rst-form-bar, rst-form-bar-note, rst-form-actions) — no
	// partial emits any of them, since they wrap a caller-composed run
	// of "field" partials rather than a single data shape. The save bar
	// is rst-form-bar and not rst-form-foot: form-foot the partial
	// emits rst-form-foot, the plain closing row, and one attribute
	// cannot carry two rules. Two adjacent [rst-field] divs exercise
	// the rst-form-flow spacing rule; the row's grown field exercises
	// rst-grow. The cancel/save pair reuses the existing buttons (Task
	// 3's ambiguity resolution: no new rst-btn variant needed).
	"form-layout": `<form rst-form-flow method="post" action="/settings">
  <div rst-field>
    <label rst-field-label for="name">Name</label>
    <input rst-input type="text" id="name" name="name">
  </div>
  <div rst-field>
    <label rst-field-label for="email">Email</label>
    <input rst-input type="email" id="email" name="email">
  </div>
  <div rst-field-row>
    <div class="rst-grow" rst-field>
      <label rst-field-label for="city">City</label>
      <input rst-input type="text" id="city" name="city">
    </div>
    <div rst-field>
      <label rst-field-label for="zip">ZIP</label>
      <input rst-input="short" type="text" id="zip" name="zip">
    </div>
  </div>
  <div rst-form-bar>
    <span rst-form-bar-note>Changes save immediately.</span>
    <div rst-form-actions>
      <a rst-btn href="/settings">Cancel</a>
      <button rst-btn="primary" type="submit">Save</button>
    </div>
  </div>
</form>`,
	// tblock reuses field-check's exact switch markup (input + a sibling
	// rst-switch-track) inside its own head, so :has() can key off the
	// same input:checked selector tokens.css already ships for the
	// switch. The body is hand-written static HTML — a caller's real
	// body would be a "field" partial render, but this sample has no
	// template engine of its own supplying that, so a plain input
	// stands in for it.
	"tblock": `<div rst-tblock>
  <label rst-tblock-head><input type="checkbox" name="notify" checked>
    <span rst-switch-track aria-hidden="true"></span>
    <span><span rst-tblock-title>Email notifications</span><span rst-tblock-desc>Sent for every reply to a thread you're in.</span></span>
  </label>
  <div rst-tblock-body>
    <div rst-field>
      <label rst-field-label for="notify-freq">Frequency</label>
      <input rst-input type="text" id="notify-freq" name="notify_freq" value="Daily digest">
    </div>
  </div>
</div>`,
	// modal route — the backdrop is marked inert (a real HTML attribute,
	// not a class tokens.css needs to style) so the page behind the
	// panel is unreachable by keyboard or screen reader while the modal
	// is open. The nav rail's current item is aria-current, matching the
	// dropdown and seg-tabs idioms. Closing is the plain rst-modal-close
	// link back to the page the backdrop already shows.
	//
	// The panel is <dialog open> — the rendered-open, non-modal dialog,
	// which is precisely what a modal-as-a-URL is: the server sends the
	// page with the dialog already open and no script ever calls
	// showModal(), so the idiom stays zero-JS while the panel gets the
	// element's dialog role for free — a role that, like every other one,
	// needs a name. aria-labelledby points at the panel's own <h2>, which
	// is already the thing on screen saying what the modal is, so the
	// name is real text in the page's own language rather than a string
	// this library would have to translate. A dialog with no accessible
	// name fails axe's aria-dialog-name and, more to the point, is
	// announced as "dialog" and nothing else.
	//
	// id="modal-title" assumes the one-dialog-per-response pattern this
	// idiom IS — a modal is its own URL, so a response carries one panel.
	// Paste this sample twice on one page and the two ids collide: give
	// each panel its own.
	//
	// Nothing moves to the top layer, so ::backdrop never paints and the
	// [rst-modal-overlay] div remains the scrim. tokens.css's dialog[rst-modal-panel] rule undoes the UA
	// dialog block (absolute positioning, auto margins, 1em padding,
	// Canvas colours) so the panel lays out exactly as it did as a div.
	"modal": `<div rst-backdrop inert>
  <div rst-page><h1>Settings</h1></div>
</div>
<div rst-modal-overlay>
  <dialog rst-modal-panel open aria-labelledby="modal-title">
    <nav>
      <a href="/settings/profile" aria-current="page">Profile</a>
      <a href="/settings/billing">Billing</a>
      <a href="/settings/notifications">Notifications</a>
    </nav>
    <section>
      <a rst-modal-close href="/settings" aria-label="Close settings">✕</a>
      <h2 id="modal-title">Profile</h2>
      <p>Update the name and photo shown across the account.</p>
    </section>
  </dialog>
</div>`,
	// help — the CSS tooltip (data-tip, shown via rst-tip::after on
	// hover/focus) is decoration only; aria-label carries the real
	// accessible name so a screen reader user gets the full sentence
	// even though the tooltip itself never reaches the accessibility
	// tree.
	"help": `<a rst-help rst-tip href="/help/orders" target="_blank" rel="noopener" aria-label="Help: orders" data-tip="About orders"><svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/></svg></a>`,
	// selbox — the label restates the row's own identity ("order
	// AB3PX"), the same disambiguation list-row-action's ActionAria and
	// row-menu's per-row aria-label already use, rather than a bare
	// "checkbox 3 of 12".
	"selbox": `<label rst-selbox><input type="checkbox" aria-label="Select order AB3PX"></label>`,
	// shell-topbar — one of the two page frames a shell puts around
	// [rst-page]: a skip link first in the DOM, a bar carrying brand, nav
	// and an account dropdown pushed to the inline end, then the page
	// column and a footer. Below 800px the tail (nav, account, locale)
	// goes behind the [rst-shell-menu] disclosure, which is a SIBLING of
	// the tail rather than its parent — a closed <details> hides its own
	// content, and the account menu must not sit inside a disclosure it
	// would close by opening. Its name is rst-shell-menu for exactly
	// that reason: <details name> exclusivity is document-wide. No partial emits
	// any of this — an app's layout template owns its own shell — so
	// this sample is the only exercise these attributes get. The nav's
	// current item is aria-current, the same signal the dropdown and
	// seg-tabs idioms already use.
	"shell-topbar": `<div rst-shell-topbar>
  <a rst-skip href="#main">Skip to content</a>
  <header rst-shell-bar><a rst-shell-brand href="/">Notes</a>
    <details rst-shell-menu name="rst-shell-menu"><summary><svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 12h16"/><path d="M4 6h16"/><path d="M4 18h16"/></svg>Menu</summary></details>
    <div rst-shell-tail>
    <nav rst-shell-nav><a href="/" aria-current="page">Home</a><a href="/archive">Archive</a></nav>
    <details rst-dropdown rst-shell-account name="rst-menus"><summary>Account<span rst-caret aria-hidden="true"><svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg></span></summary>
      <div rst-dropdown-menu><a href="/settings">Settings</a></div></details>
    </div>
  </header>
  <main rst-page id="main">Content.</main>
  <footer rst-shell-foot>Made with rastrillo</footer>
</div>`,
	// shell-sidebar — the same frame with a rail instead of a bar. The
	// narrow-screen disclosure is a native <details> strip whose open
	// state reveals the rail (the adjacent-sibling selector in
	// tokens.css), so the shell stays zero-JS like every other idiom
	// here. Its summary carries the same `menu` icon the topbar's does,
	// aria-hidden beside its own visible label. The rail's own [rst-page] still wraps the content, so a
	// screen's markup is identical in either shell.
	"shell-sidebar": `<div rst-shell-sidebar>
  <a rst-skip href="#main">Skip to content</a>
  <details rst-shell-chrome><summary><svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 12h16"/><path d="M4 6h16"/><path d="M4 18h16"/></svg>Menu</summary></details>
  <aside rst-shell-rail><a rst-shell-brand href="/">Notes</a>
    <nav rst-shell-nav><span rst-shell-group>Work</span><a href="/" aria-current="page">Dashboard</a><a href="/reports">Reports</a></nav>
  </aside>
  <main rst-shell-main id="main"><div rst-page>Content.</div></main>
</div>`,
}

// Styleguide returns the canonical markup for every markup idiom —
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
