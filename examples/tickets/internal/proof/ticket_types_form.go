package proof

import (
	"errors"
	"fmt"
	"maps"
	"net/http"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/form"
	"amadan.net/rastrillo/rastrillo/op"
	"amadan.net/rastrillo/rastrillo/ui"
	ops_ticket_types "tickets/gen/ops/ticket_types"
)

// currencyChangedKey is the price's message once the currency has
// moved twice in one save: by then the person has to look at the
// amount again, and a third try could move under them too.
const currencyChangedKey = "rastrillo.ui.money_currency_changed"

// basicsForm is the edit form's basics as the person sent them, beside
// the record they were read against. Showing the form again needs
// both: what the person typed, and the record's values and currency
// for everything the basics form does not carry.
type basicsForm struct {
	record ops_ticket_types.TicketType
	parsed *form.Parsed
	input  ops_ticket_types.UpdateInput

	// currencyMoved keeps the price as typed when the form is shown
	// again, because the currency changed under it twice and the
	// person should check the amount, not lose it.
	currencyMoved bool
}

// readBasics reads the basics against record. A field that does not
// parse also runs the operation's own checks, so the person sees every
// problem in one pass, and the parse's message wins a field both
// report, as the generated action merges them.
func readBasics(
	visit *action.Visit, record ops_ticket_types.TicketType,
) (*basicsForm, error) {
	code := currencyOf(record.Price)
	parsed, err := visit.ReadForm(
		form.Field{Name: "Name"},
		form.Field{
			Name: "Price", Kind: form.Money, Currency: code,
			LocaleEcho: true, Required: true,
			Stored: minorOf(record.Price),
		},
		form.Field{Name: "Status"},
	)
	if parsed == nil {
		// Only a body that could not be read leaves no parsed form,
		// and RefusedShowing answers that 400 without ShowAgain.
		return nil, err
	}
	basics := &basicsForm{record: record, parsed: parsed}
	basics.input = ops_ticket_types.UpdateInput{
		Name:   sent(visit, "Name", parsed.String("Name")),
		Price:  amount(parsed, "Price", code),
		Status: sent(visit, "Status", parsed.String("Status")),
	}
	var invalid op.Invalid
	if !errors.As(err, &invalid) {
		return basics, err
	}
	fields := ops_ticket_types.ValidateUpdate(basics.input)
	maps.Copy(fields, invalid.Fields)
	return basics, op.Invalid{Fields: fields}
}

// currencyMovedAgain is the refusal for a second move: a field error
// on the price, so the person is shown the form, not a conflict page.
func (basics *basicsForm) currencyMovedAgain() error {
	basics.currencyMoved = true
	return op.Invalid{Fields: form.Errors{"Price": currencyChangedKey}}
}

// ShowAgain draws the edit form with what the person sent over the
// record's values, so the advanced fields, which this form does not
// post, still show what is stored.
func (basics *basicsForm) ShowAgain(
	visit *action.Visit, status int, fieldErrors form.Errors,
) {
	price, err := basics.price(visit.Request())
	if visit.Refused(err) {
		return
	}
	fields := map[string]string{
		"Name":        basics.record.Name,
		"Status":      basics.record.Status,
		"MaxPerOrder": basics.record.MaxPerOrder,
	}
	maps.Copy(fields, basics.parsed.Echo())
	request := visit.Request()
	visit.Render("ticket_types/form", status, formView{
		Localized:      ui.ForRequest(request),
		Fields:         fields,
		Errors:         fieldErrors,
		ErrorArgs:      basics.parsed.ErrorArgs(),
		Money:          map[string]form.MoneyInput{"Price": price},
		BasicsAction:   basics.action(request, editBasics),
		AdvancedAction: basics.action(request, editAdvanced),
	})
}

// price is the money input to draw. The parsed form gives the person's
// text with its locale, currency and verified seed; when it cannot
// (the locale or seed was refused, so the text's meaning is unknown)
// the input is seeded from the record instead.
func (basics *basicsForm) price(
	request *http.Request,
) (form.MoneyInput, error) {
	code := currencyOf(basics.record.Price)
	if basics.currencyMoved {
		return form.MoneyInput{
			Value:    basics.parsed.Echo()["Price"],
			Locale:   request.PostFormValue("Price.locale"),
			Currency: code,
		}, nil
	}
	locale := rastrillo.FormatLocale(request)
	price, reseed := basics.parsed.MoneyInput("Price", locale)
	if !reseed {
		return price, nil
	}
	price, err := form.SeedMoney(
		locale, minorOf(basics.record.Price), code, false)
	if err != nil {
		return price, fmt.Errorf("ticket_types %d: Price in %s: %w",
			basics.record.ID, code, err)
	}
	return price, nil
}

// action is a form's action attribute. A page's body is not rewritten
// the way a redirect's Location is, so it carries the tenant's base.
func (basics *basicsForm) action(
	request *http.Request, path action.Path,
) string {
	return basepath.Path(request, path.For(basics.record.ID))
}

// formView is the data ticket_types/form.html declares in its header.
// IsNew stays although it is always false here, because the template
// reads it and a missing field fails the render.
type formView struct {
	ui.Localized
	IsNew          bool
	Fields         map[string]string
	Errors         map[string]string
	ErrorArgs      map[string]map[string]any
	Money          map[string]form.MoneyInput
	BasicsAction   string
	AdvancedAction string
}

// sent records whether the field was posted at all, so the update
// changes only what the form sent and an emptied field is not taken
// for one the form left out.
func sent(visit *action.Visit, name, value string) op.Opt[string] {
	_, posted := visit.Request().PostForm[name]
	return op.Opt[string]{Value: value, Set: posted}
}

// amount leaves the price unset when the field is blank or refused,
// so a refused amount can never be stored.
func amount(parsed *form.Parsed, name, code string) op.Opt[op.Money] {
	minor, present := parsed.Minor(name)
	if !present {
		return op.Opt[op.Money]{}
	}
	return op.Some(op.Money{Amount: minor, Currency: code})
}

// currencyOf falls back to USD, the resource's default, for a record
// stored before it had a price.
func currencyOf(money *op.Money) string {
	if money != nil {
		return money.Currency
	}
	return "USD"
}

func minorOf(money *op.Money) *int64 {
	if money == nil {
		return nil
	}
	return &money.Amount
}
