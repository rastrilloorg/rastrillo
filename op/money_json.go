package op

import (
	"errors"

	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// errNoAmount is what a money object with no amount earns. DecodeInput
// finds it with errors.Is and answers msgNoAmount under the member.
var errNoAmount = errors.New("amount is required")

// UnmarshalJSONFrom reads {"amount": <integer>, "currency": "<code>"}.
// amount is required: decoded into a plain int64, {"currency": "USD"}
// would read as zero, and a price of nothing would be stored as if the
// client had sent it. currency may be left out; the operation fills it.
// Any other member is refused.
func (m *Money) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var raw struct {
		Amount   *int64 `json:"amount"`
		Currency string `json:"currency"`
	}
	if err := json.UnmarshalDecode(dec, &raw, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if raw.Amount == nil {
		return errNoAmount
	}
	*m = Money{Amount: *raw.Amount, Currency: raw.Currency}
	return nil
}

var _ json.UnmarshalerFrom = (*Money)(nil)
