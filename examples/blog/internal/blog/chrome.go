package blog

import "amadan.net/rastrillo/rastrillo/ui"

// The topbar shell's hierarchy: where each page sits, and the Menu
// page's view model.

// Chrome is where a page sits in the topbar shell: View is "index" on
// the Menu page and empty on every other, which is a content page; Up
// is the content page's way up, "" for the Menu page itself (the
// layout's default); UpLabel is what that link says, "" for Menu; and
// Section is the nav link marked current, "blog" or "admin".
type Chrome struct {
	View, Up, UpLabel, Section string
}

// chromeFor places a page by its name rather than by a block each
// template defines, because the generated screens (gen/templates)
// cannot carry blocks of their own. A top-level page goes up to its
// own row on the Menu page, so focus lands there even with scripts
// off; a page under one goes up to that page.
func chromeFor(page string) Chrome {
	switch page {
	case "menu":
		return Chrome{View: "index"}
	case "index":
		return Chrome{Up: "/menu#nav-blog", Section: "blog"}
	case "post":
		return Chrome{Up: "/", UpLabel: "Blog", Section: "blog"}
	case "posts/list":
		return Chrome{Up: "/menu#nav-admin", Section: "admin"}
	}
	return Chrome{Up: "/admin/posts", UpLabel: "Posts admin",
		Section: "admin"}
}

// MenuView is GET /menu, the Menu page; see HomeView's doc comment on
// the embedded ui.Localized.
type MenuView struct {
	ui.Localized
	Head Head
}
