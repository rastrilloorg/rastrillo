# 🤖 clientip

`amadan.net/rastrillo/rastrillo/clientip`

The address a request came from, when it reached your app through
proxies. Use it for anything keyed on the visitor's address, such as a
per-IP rate limit or a log line.

`From` reads `X-Forwarded-For` from the right. Each proxy adds the
address it received the request from to the end of the header, so only
the last elements were written by proxies you run. Everything to their
left is whatever the client sent. Trust that, and one client can look
like a new visitor on every request.

## From

```go
func From(r *http.Request, hops int) string
```

`hops` is how many proxies you run in front of the app. `From` returns
the element that many places from the end. On CARLOS, pass 1: the edge
adds the visitor's address as the last element.

With `hops` of 0, the header is ignored and `From` returns the
connection's address. Use 0 when nothing is in front of the app.

If the header is missing, or has fewer elements than `hops`, `From`
returns the connection's address too. That request did not come through
your proxies, and the client does not get to say otherwise.

Never set `hops` higher than the number of proxies really there. One too
many and `From` trusts an address the client wrote.

## ParseHops

```go
const DefaultHops = 1

func ParseHops(raw string) (int, error)
```

`ParseHops` reads a hop count from configuration, such as an environment
variable. Empty gives `DefaultHops`, which is 1. Anything that is not a
whole number of zero or more also gives `DefaultHops`, with an error for
you to log.
