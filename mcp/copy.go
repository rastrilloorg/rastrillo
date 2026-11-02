//rastrillo:data the approved copy, kept verbatim from docs/superpowers/plans/2026-10-25-mcp-copy.md

package mcp

// instructionsText is the framework's part of the instructions sent on
// connect (spec §7); the app's Config.Instructions follows it.
const instructionsText = `Each tool here runs one operation in the app for the person who connected you.

- Send money as {"amount": <integer in minor units>, "currency": "<ISO 4217 code>"}. Do not send a float.
- Send times in RFC 3339 format in UTC.
- Use IDs returned by list or get. Do not guess IDs.
- Before you write, get the item. Change only the fields you intend to change.
- To get the next page, pass next_after as after. Stop when next_after is null.
- If a tool description asks you to confirm an action, ask the person first using the sentence provided.
- Do not retry a write without checking its result. If the call times out or is cut short, read the item before trying again.`

// confirmLine leads a tool's Confirm sentence. Its trailing space is
// part of the approved string.
const confirmLine = "Before calling, ask the person to confirm: "

// msgNoStreams answers a GET, a batch and subscriptions/listen.
const msgNoStreams = "Send one JSON-RPC message per HTTP POST. " +
	"Do not send batches, use GET, or request subscriptions."
