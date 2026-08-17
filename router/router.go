// Package router implements an in-process path router.
//
// Callers register route patterns that may contain static segments,
// named parameters (":id") and a trailing catch-all ("*path"), each bound
// to a handler label. Matching a request path returns the best (most
// specific) matching label together with the extracted parameters.
package router

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// segKind identifies the kind of a single pattern segment.
type segKind int

const (
	segStatic    segKind = iota // literal text, e.g. "users"
	segParam                    // named parameter, e.g. ":id"
	segCatchAll                 // catch-all, e.g. "*path"
)

// segment is one parsed element of a route pattern.
type segment struct {
	kind  segKind
	value string // static text, parameter name (without ':') or catch-all name (without '*')
}

// Route is a registered pattern bound to a handler label.
type Route struct {
	Pattern  string
	Label    string
	segments []segment
}

// Match holds the result of matching a path against the route table.
type Match struct {
	Label  string
	Params map[string]string
}

// ErrEmptyLabel is returned when a route is registered with an empty label.
var ErrEmptyLabel = errors.New("router: empty label")

// Router holds the registered routes in registration order. It is safe for
// concurrent use: Register takes the write lock and Match the read lock.
type Router struct {
	mu    sync.RWMutex
	routes []Route
	seen   map[string]struct{} // audit of matched paths
}

// New returns an empty Router.
func New() *Router {
	return &Router{seen: map[string]struct{}{}}
}

// Register adds a route pattern bound to a label.
//
// It returns an error if the label is empty, the pattern is malformed, a
// catch-all appears anywhere but the final segment, or the pattern is
// structurally identical to one already registered (a conflict).
func (rt *Router) Register(pattern, label string) error {
	if label == "" {
		return ErrEmptyLabel
	}
	segs, err := parsePattern(pattern)
	if err != nil {
		return err
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, ex := range rt.routes {
		if conflicts(segs, ex.segments) {
			// An exact re-registration of the same pattern with the same
			// label is treated as a no-op rather than a conflict.
			if label == ex.Label {
				return nil
			}
			return fmt.Errorf("router: pattern %q conflicts with registered %q", pattern, ex.Pattern)
		}
	}
	rt.routes = append(rt.routes, Route{Pattern: pattern, Label: label, segments: segs})
	return nil
}

// Routes returns the registered patterns and their labels, in order.
func (rt *Router) Routes() []string {
	var out []string
	for _, r := range rt.routes {
		out = append(out, r.Pattern+" -> "+r.Label)
	}
	return out
}

// Match returns the best matching route for path, or ok=false if no route
// matches or the path is invalid. When several routes match, the most
// specific one wins: static beats parameter beats catch-all, compared
// segment by segment from the left; a shorter static-only pattern beats a
// longer one that continues with a catch-all.
func (rt *Router) Match(path string) (Match, bool) {
	pathSegs, ok := parsePath(path)
	if !ok {
		return Match{}, false
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	rt.seen[path] = struct{}{} // audit: record the matched path
	var best *Route
	var bestParams map[string]string
	for i := range rt.routes {
		r := &rt.routes[i]
		params, ok := r.match(pathSegs)
		if !ok {
			continue
		}
		if best == nil || moreSpecific(r.segments, best.segments) {
			best = r
			bestParams = params
		}
	}
	if best == nil {
		return Match{}, false
	}
	return Match{Label: best.Label, Params: bestParams}, true
}

// match attempts to match a path (already split into segments) against the
// route. On success it returns the extracted parameters.
func (r Route) match(pathSegs []string) (map[string]string, bool) {
	params := map[string]string{}
	pi := 0 // index into pathSegs
	for _, seg := range r.segments {
		switch seg.kind {
		case segStatic:
			if pi >= len(pathSegs) || !strings.EqualFold(pathSegs[pi], seg.value) {
				return nil, false
			}
			pi++
		case segParam:
			if pi >= len(pathSegs) || pathSegs[pi] == "" {
				return nil, false
			}
			params[seg.value] = pathSegs[pi]
			pi++
		case segCatchAll:
			// Consume all remaining segments; zero or more is allowed.
			rest := strings.Join(pathSegs[1:], "/")
			if seg.value != "" {
				params[seg.value] = rest
			}
			pi = len(pathSegs)
		}
	}
	// Every pattern segment must be consumed and, unless the last was a
	// catch-all, the path must be fully consumed too.
	if pi != len(pathSegs) {
		return nil, false
	}
	return params, true
}

// parsePath splits a request path into segments after normalization.
// It returns ok=false if the path is empty, does not start with '/',
// or contains an empty segment (consecutive slashes).
func parsePath(p string) ([]string, bool) {
	if p == "" || p[0] != '/' {
		return nil, false
	}
	if strings.Contains(p, "//") {
		return nil, false
	}
	// strip a single trailing slash, keep root.
	if len(p) > 1 && p[len(p)-1] == '/' {
		p = p[:len(p)-1]
	}
	if p == "/" {
		return []string{}, true
	}
	return strings.Split(p[1:], "/"), true
}

// parsePattern parses a route pattern into segments, applying the same
// normalization as parsePath. A catch-all must be the final segment.
func parsePattern(pattern string) ([]segment, error) {
	if pattern == "" || pattern[0] != '/' {
		return nil, fmt.Errorf("router: pattern must start with '/': %q", pattern)
	}
	if strings.Contains(pattern, "//") {
		return nil, fmt.Errorf("router: pattern has empty segment: %q", pattern)
	}
	if len(pattern) > 1 && pattern[len(pattern)-1] == '/' {
		pattern = pattern[:len(pattern)-1]
	}
	if pattern == "/" {
		return []segment{}, nil
	}
	parts := strings.Split(pattern[1:], "/")
	segs := make([]segment, 0, len(parts))
	for i, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("router: pattern has empty segment: %q", pattern)
		}
		switch {
		case strings.HasPrefix(part, ":"):
			name := part[1:]
			if name == "" {
				return nil, fmt.Errorf("router: empty parameter name in pattern %q", pattern)
			}
			segs = append(segs, segment{kind: segParam, value: name})
		case strings.HasPrefix(part, "*"):
			if i != len(parts)-1 {
				return nil, fmt.Errorf("router: catch-all must be the last segment in pattern %q", pattern)
			}
			segs = append(segs, segment{kind: segCatchAll, value: part[1:]})
		default:
			segs = append(segs, segment{kind: segStatic, value: part})
		}
	}
	return segs, nil
}

// kindRank returns the specificity rank of a segment kind: the higher the
// rank, the more specific. Static beats parameter beats catch-all.
func kindRank(k segKind) int {
	switch k {
	case segStatic:
		return 3
	case segParam:
		return 2
	case segCatchAll:
		return 1
	}
	return 0
}

// conflicts reports whether two parsed patterns are structurally identical:
// equal segment kinds at every position and equal static text where static.
// Two such patterns can match the same path with no specificity tiebreaker.
func conflicts(a, b []segment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].kind != b[i].kind {
			return false
		}
		if a[i].kind == segStatic && a[i].value != b[i].value {
			return false
		}
	}
	return true
}

// moreSpecific reports whether pattern a is strictly more specific than b.
// It is only called on two patterns that both matched the same path.
func moreSpecific(a, b []segment) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		ra, rb := kindRank(a[i].kind), kindRank(b[i].kind)
		if ra != rb {
			return ra > rb
		}
	}
	if len(a) == len(b) {
		return false // structurally identical: not strictly more specific
	}
	// Shared prefix identical and lengths differ. Both matched the path, so
	// the longer pattern's extra segments must be a catch-all (the only kind
	// that can match zero remaining segments). The shorter pattern is more
	// specific because it ended without a wildcard.
	return len(a) < len(b)
}
