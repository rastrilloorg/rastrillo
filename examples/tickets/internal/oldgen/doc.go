// Package oldgen is the code `rastrillo generate` wrote for ticket_types
// before money had a currency, frozen: editbasics is the edit-basics
// POST handler and store/ticket_types the sqlc store it called, copied
// unchanged except for the store's import path.
//
// It is kept so a test can run the real old handler against a database
// the new Boot has upgraded (ticketstest's upgrade_test.go). That
// handler parses an unchanged "1200" yen as 1200 cents and would store
// 120000; the upgrade drops the column it writes, so it must fail
// instead. A hand-written stand-in could pass that test while the real
// old code would not. Never regenerate or edit these files.
package oldgen
