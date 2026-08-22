// Package scope makes the right query the short query: every model
// owned by a user (or team) is read through its owner filter, and a
// row that isn't yours is a row that doesn't exist — handlers answer
// 404, never 403 (matching view.ParseID's rule: a URL that was never
// yours was never a URL).
package scope

import (
	"regexp"
	"strconv"

	"gorm.io/gorm"
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Owned scopes g to rows whose user_id is owner — the convention
// column. For another owner column, OwnedBy.
func Owned(g *gorm.DB, owner int64) *gorm.DB {
	return g.Where("user_id = ?", owner)
}

// OwnedBy scopes g by an arbitrary owner column. The column must be a
// plain lower_snake identifier — it is interpolated into SQL, so
// anything else panics loudly at development time rather than parsing
// as SQL.
func OwnedBy(g *gorm.DB, column string, owner any) *gorm.DB {
	if !identifier.MatchString(column) {
		panic("rastrillo/scope: OwnedBy column must be a plain identifier, got " + strconv.Quote(column))
	}
	return g.Where(column+" = ?", owner)
}
