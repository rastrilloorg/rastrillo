package chat

import "time"

// Models is every model rastrillo migration generate manages; migration
// check fails the gate when it and migrations/ disagree.
var Models = []any{
	&Note{},
	&Room{},
	&Message{},
}

// Note is an example model: rename it, replace it, add more beside it.
// Query it through owned (auth.go), and never bind a request
// body onto a model; AGENTS.md says why.
type Note struct {
	ID        int64
	Owner     string `gorm:"index"`
	Title     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Room is a place to talk. Admins make, rename and delete rooms.
type Room struct {
	ID        int64
	Name      string `gorm:"uniqueIndex"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Message is one line somebody posted in a room.
type Message struct {
	ID        int64
	RoomID    int64 `gorm:"index"`
	Sender    string
	Text      string
	CreatedAt time.Time
}
