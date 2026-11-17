package designsystem

// fieldNumberDoc is field-number's place on the Form page: the bare
// field, a hint, and the refusal a person sees for text with two
// readings, which is the reason the field exists. It sits in its own
// function because families is already past the function limit.
func fieldNumberDoc() partialDoc {
	return partialDoc{
		Name: "field-number",
		Blurb: "A number, read in the language the page was drawn in. " +
			"The language is sent with the form, so 1.500 is never " +
			"read the other way. Pair it with form.Number on the server.",
		Wrap: wrapForm,
		States: []sample{
			{State: "Bare", Data: map[string]any{
				"Name": "fn_bare", "Label": "Seats", "Locale": "en",
			}},
			{State: "With a hint", Data: map[string]any{
				"Name": "fn_hint", "Label": "Seats", "Locale": "en",
				"Hint": "For example, 1200.5.",
			}},
			{State: "With an error", Data: map[string]any{
				"Name": "fn_error", "Label": "Seats", "Locale": "en",
				"Value": "1.500",
				"Error": "This could be 1.5 or 1500. " +
					"Enter the one you mean.",
			}},
		},
	}
}
