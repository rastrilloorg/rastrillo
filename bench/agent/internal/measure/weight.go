// Package measure weighs and audits a deployed app's pages the way a
// first-time visitor's browser meets them.
package measure

type Kind string

const (
	HTML      Kind = "html"
	CSS       Kind = "css"
	JS        Kind = "js"
	Font      Kind = "font"
	Image     Kind = "image"
	OtherKind Kind = "other"
)

// Weight is what a page cost to load: Wire as sent (compressed),
// Decoded as the browser had to parse it.
type Weight struct{ Wire, Decoded map[Kind]int }

func (w Weight) WireTotal() int {
	total := 0
	for _, size := range w.Wire {
		total += size
	}
	return total
}

// NetworkEvent is one finished request, as CDP reported it.
type NetworkEvent struct {
	RequestID string
	Type      string // CDP resource type
	Encoded   int
	Decoded   int
	Host      string
}

var kindOf = map[string]Kind{
	"Document": HTML, "Stylesheet": CSS, "Script": JS,
	"Font": Font, "Image": Image,
}

func Tally(events []NetworkEvent) Weight {
	weight := Weight{Wire: map[Kind]int{}, Decoded: map[Kind]int{}}
	for _, event := range events {
		kind, known := kindOf[event.Type]
		if !known {
			kind = OtherKind
		}
		weight.Wire[kind] += event.Encoded
		weight.Decoded[kind] += event.Decoded
	}
	return weight
}
