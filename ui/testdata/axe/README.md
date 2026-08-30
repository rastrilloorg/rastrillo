# axe-core, vendored

`axe.min.js` is [axe-core](https://github.com/dequelabs/axe-core), Deque's
accessibility rules engine, vendored so the WCAG gate runs from the checkout
and not from the network.

    version   4.10.3
    file      axe.min.js
    sha256    880970c081707360e64f34cea25ff91892f5bc95675b0776925b9709dd8a68bb
    source    https://cdnjs.cloudflare.com/ajax/libs/axe-core/4.10.3/axe.min.js
    verified  byte-identical to https://cdn.jsdelivr.net/npm/axe-core@4.10.3/axe.min.js
    licence   MPL-2.0 (the header comment in the file is the notice)

## This is test data

It is read by `internal/designsystem/a11y_test.go` (build tag `browser`), which
injects it into a headless Chromium and scans the committed gallery. It is not
embedded, not served, not part of `ui.ShimJS()`, and never reaches
`docs/design-system/`. Two untagged tests assert all of that, so they run on
every `go test ./...` and not only when a browser is around:
`TestVendoredAxeIsNotAShippedAsset` in `ui/axe_test.go` holds the library's
assets and the embedded template tree, and `TestVendoredAxeStaysOutOfTheTree`
in `internal/designsystem` holds the 189 published files.

## Upgrading

Fetch the new version from both CDNs above, check the two copies are identical,
replace the file, and update the version and sha256 in this README —
`TestVendoredAxeIsThePinnedVersion` reads both out of this file and fails if
either stops describing what is on disk. Then run the scan: a new axe-core
finds things the old one did not, and those findings are the point.

    go test -tags browser -run A11y ./internal/designsystem/
