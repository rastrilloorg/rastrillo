# 🤖 eventlog

`github.com/carlosframework/rastrillo/eventlog`

The mergeable store shape: a command never `UPDATE`s. It appends an
immutable event to a resource's stream, and a pure `Derive` fold
recomputes the read model.

Because the fold is pure and total over the history, replay works for
free — truncate and refold — and multi-edge works for free: the visible
state is the merge of many single-writer streams.

This is the app-side half of the platform's `mergeable` contract, and
[Manifests](/docs/manifests) can generate a store on it with
`store = "mergeable"`.

## The table

One table holds every stream. Each row is one immutable event, keyed
`(stream, writer, seq)`: `writer` is the appending instance's identity,
`seq` its own dense counter. A writer never rewrites its history.

`eventlog.Schema` is the migration set.

## Open, Append, Events

```go
func Open(db *sql.DB) (*Log, error)
func (l *Log) Append(ctx context.Context, stream string, e Event) error
func (l *Log) Events(ctx context.Context, stream string) ([]Event, error)
func (l *Log) EventsByPrefix(ctx context.Context, prefix string) ([]Event, error)
```

`EventsByPrefix` escapes its `LIKE` pattern, so a stream name containing
`%` or `_` matches literally rather than as a wildcard.

## The merge order

The merged read order is `(lamport, ts, writer, seq)`: a total order,
deterministic across edges by construction. Any two logs holding the
same events in any ingest order read them back identically, which is
what makes `Derive` converge.

`Log.Order` is the app-supplied comparator seam for a different merge
rule. The lamport default is the framework's answer until a real one is
confirmed against this shape.

## Derive

```go
func Derive[S any](events []Event, fold func(S, Event) S, zero S) S
```

A pure generic fold, and you should keep yours pure. The whole design
rests on recomputing state from history at any time, and a fold that
reads the clock or the database cannot be replayed.

`ErrDiverged` reports a fold that disagreed with a recorded result.

## Ingest

```go
func (l *Log) Ingest(ctx context.Context, events []Event) error
```

Idempotent, and the seam the platform's transport will call.
Re-ingesting an event you already hold is a no-op instead of a
duplicate, which is what lets a transport retry freely.

## LocalWriter

```go
func LocalWriter(db *sql.DB) (string, error)
```

This instance's writer identity, stable across restarts because it is
persisted in its own table.

## What is deliberately not here

- **Transport.** Edge sync is the platform's territory, and `Ingest` is
  the seam it will call. Until it lands, ids minted by a generated
  mergeable store are writer-local.
- **Snapshots.** Replay is cheap until proven otherwise.
- **Any `UPDATE` or `DELETE` verb.** Deletion is an appended tombstone
  event, and what it means is up to your `Derive`.
