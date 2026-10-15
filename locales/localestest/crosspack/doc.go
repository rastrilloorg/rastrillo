// Package crosspack links only the en, fr and no packs, to check that a
// locale whose CLDR parent is in another pack falls back to that linked
// parent when its own pack is absent: hi-Latn to en-IN, ht to fr-HT, nb
// and nn to no.
package crosspack
