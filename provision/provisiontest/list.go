package provisiontest

import (
	"encoding/base64"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// MakeReady ends an existing instance's pending answers at once. Unready
// only affects instances created after it is called.
func (platform *Platform) MakeReady(host string) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	if found := platform.instances[host]; found != nil {
		found.pending = 0
	}
}

// Sleep makes the instance's health Asleep. It still answers if asked,
// because the real edge wakes it.
func (platform *Platform) Sleep(host string) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	if found := platform.instances[host]; found != nil {
		found.asleep = true
	}
}

// Fail makes the instance's health Failed and its edge answers 502.
func (platform *Platform) Fail(host string) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	if found := platform.instances[host]; found != nil {
		found.failed = true
	}
}

// CatchAll makes the edge serve <label>.<parent> for each of the app's
// instances, where label is the first label of the instance's host. The
// app's handler is told the instance's own host, as on the real edge.
func (platform *Platform) CatchAll(account, app, parent string) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.catchAlls[parent] = appKey{account, app}
}

// ScriptList is Script for list calls.
func (platform *Platform) ScriptList(replies ...Reply) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.listReply = append(platform.listReply, checkReplies(replies)...)
}

const (
	// instanceListMax is apiinstances.go's cap on the records one list
	// answer is built from.
	instanceListMax = 500
	// maxListLimit is instanceListMax less one: the console honours a
	// limit below the cap and ignores any other.
	maxListLimit = instanceListMax - 1
)

type listedView struct {
	record
	ServedBy   []servedBy `json:"served_by,omitempty"`
	Gated      bool       `json:"gated"`
	PublicHost string     `json:"public_host,omitempty"`
	Health     string     `json:"health"`
}

// public_host is left empty: the console sends it only for a host the
// account has a name host for, and "" when it would be the host itself
// (canonical.go:170-176, apiinstances.go:190).

// health is what the console reads of an instance from its own reports,
// never by asking the instance: it is still being set up while pending
// answers remain.
func (found *instance) health() string {
	switch {
	case found.failed:
		return "Failed"
	case found.pending > 0:
		return "Setting up"
	case found.asleep:
		return "Asleep"
	}
	return "Running"
}

// list is handleAPIInstanceList (apiinstances.go:1252-1400): the first
// instanceListMax hosts in order, q a substring of the host, and a cursor
// that resumes after the host it names.
func (platform *Platform) list(
	response http.ResponseWriter, request *http.Request, _ *exchange,
	key appKey,
) {
	hosts := append([]string(nil), platform.app(key).hosts...)
	sort.Strings(hosts)
	// The platform reads at most instanceListMax records, before it
	// filters or pages, and says so with truncated.
	truncated := len(hosts) > instanceListMax
	if truncated {
		hosts = hosts[:instanceListMax]
	}
	params := request.URL.Query()
	substring := strings.TrimSpace(params.Get("q"))
	limit := maxListLimit + 1
	size, err := strconv.Atoi(params.Get("limit"))
	if err == nil && size > 0 && size <= maxListLimit {
		limit = size
	}
	var after string
	raw, err := base64.RawURLEncoding.DecodeString(params.Get("cursor"))
	if err == nil {
		after = string(raw)
	}
	counts := map[string]int{}
	var matched []listedView
	for _, host := range hosts {
		found := platform.instances[host]
		counts[found.health()]++
		if substring != "" && !strings.Contains(host, substring) {
			continue
		}
		matched = append(matched, listedView{
			record: found.rec, ServedBy: []servedBy{{"box1"}},
			Health: found.health(),
		})
	}
	page, next := pageOf(matched, after, limit)
	writeJSON(response, http.StatusOK, map[string]any{
		"instances": page, "truncated": truncated, "total": len(matched),
		"counts": counts, "next_cursor": next,
	})
}

// pageOf is the records after the cursor's host, at most limit of them,
// and the cursor of the next page ("" on the last).
func pageOf(
	matched []listedView, after string, limit int,
) ([]listedView, string) {
	start := sort.Search(len(matched), func(index int) bool {
		return matched[index].Host > after
	})
	end, next := start+limit, ""
	if end < len(matched) {
		last := matched[end-1].Host
		next = base64.RawURLEncoding.EncodeToString([]byte(last))
	} else {
		end = len(matched)
	}
	if start == end {
		return []listedView{}, next
	}
	return matched[start:end], next
}

// catchAllInstance finds the instance a <label>.<parent> host names under
// a catch-all claim ("Instance catch-alls"): the app's instance whose
// host starts with that label. A host that is itself an instance, or that
// has an exact claim, is not shadowed by it: an exact claim on a derived
// name always wins, whoever holds it.
func (platform *Platform) catchAllInstance(host string) (*instance, bool) {
	if _, ok := platform.instances[host]; ok {
		return nil, false
	}
	if _, ok := platform.claims[host]; ok {
		return nil, false
	}
	label, parent, ok := strings.Cut(host, ".")
	key, claimed := platform.catchAlls[parent]
	if !ok || !claimed {
		return nil, false
	}
	for _, instanceHost := range platform.app(key).hosts {
		first, _, _ := strings.Cut(instanceHost, ".")
		if first == label {
			return platform.instances[instanceHost], true
		}
	}
	return nil, false
}

// instanceRoute is how the edge answers for an instance: a failed one with
// a 502, one still being set up with the pending page, else its app.
func (platform *Platform) instanceRoute(found *instance, as string) route {
	if found.failed {
		return route{failed: true}
	}
	if found.pending > 0 {
		found.pending--
		return route{pending: true}
	}
	return platform.routeOf(found.key, as)
}
