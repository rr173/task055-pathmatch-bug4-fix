package router

import (
	"reflect"
	"testing"
)

func TestParsePath(t *testing.T) {
	cases := []struct {
		in   string
		segs []string
		ok   bool
	}{
		{"/", []string{}, true},
		{"/users", []string{"users"}, true},
		{"/users/", []string{"users"}, true},
		{"/a/b/c", []string{"a", "b", "c"}, true},
		{"", nil, false},
		{"noslash", nil, false},
		{"/a//b", nil, false},
		{"//", nil, false},
		{"/a//", nil, false},
		{"//a", nil, false},
	}
	for _, c := range cases {
		segs, ok := parsePath(c.in)
		if ok != c.ok {
			t.Errorf("parsePath(%q) ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if ok && !reflect.DeepEqual(segs, c.segs) {
			t.Errorf("parsePath(%q) segs=%v want %v", c.in, segs, c.segs)
		}
	}
}

func TestParsePatternErrors(t *testing.T) {
	bad := []string{
		"",
		"noslash",
		"/bad/*x/y", // catch-all not last
		"/users/:",  // empty parameter name
		"/a//b",     // empty segment
		"/a//",      // trailing empty segment
	}
	for _, p := range bad {
		if _, err := parsePattern(p); err == nil {
			t.Errorf("parsePattern(%q) expected error", p)
		}
	}
}

func TestParsePatternOK(t *testing.T) {
	cases := []struct {
		pat  string
		segs []segment
	}{
		{"/", []segment{}},
		{"/users", []segment{{segStatic, "users"}}},
		{"/users/", []segment{{segStatic, "users"}}},
		{"/users/:id", []segment{{segStatic, "users"}, {segParam, "id"}}},
		{"/files/*path", []segment{{segStatic, "files"}, {segCatchAll, "path"}}},
		{"/static/*", []segment{{segStatic, "static"}, {segCatchAll, ""}}},
	}
	for _, c := range cases {
		segs, err := parsePattern(c.pat)
		if err != nil {
			t.Fatalf("parsePattern(%q) error: %v", c.pat, err)
		}
		if !reflect.DeepEqual(segs, c.segs) {
			t.Errorf("parsePattern(%q) = %+v want %+v", c.pat, segs, c.segs)
		}
	}
}

func TestRegisterAndMatch(t *testing.T) {
	rt := New()
	must := func(pat, label string) {
		t.Helper()
		if err := rt.Register(pat, label); err != nil {
			t.Fatalf("register %q: %v", pat, err)
		}
	}
	must("/", "root")
	must("/users", "users")
	must("/users/list", "users-list")
	must("/users/:id", "user")
	must("/users/:id/profile", "profile")
	must("/files/*path", "files")
	must("/files", "files-root")

	cases := []struct {
		path   string
		label  string
		params map[string]string
	}{
		{"/", "root", map[string]string{}},
		{"/users", "users", map[string]string{}},
		{"/users/", "users", map[string]string{}},                       // trailing slash normalized
		{"/users/list", "users-list", map[string]string{}},              // static beats parameter
		{"/users/42", "user", map[string]string{"id": "42"}},
		{"/users/42/profile", "profile", map[string]string{"id": "42"}},
		{"/files", "files-root", map[string]string{}},                   // shorter static beats catch-all
		{"/files/", "files-root", map[string]string{}},                  // normalized then shorter static wins
		{"/files/a/b/c", "files", map[string]string{"path": "a/b/c"}},
		{"/files/a", "files", map[string]string{"path": "a"}},
		{"/unknown", "", nil},
		{"/users//x", "", nil},  // invalid path
		{"bad", "", nil},        // invalid path
		{"//", "", nil},         // invalid path
	}
	for _, c := range cases {
		m, ok := rt.Match(c.path)
		if !ok {
			if c.label != "" {
				t.Errorf("Match(%q) no match, want %s", c.path, c.label)
			}
			continue
		}
		if c.label == "" {
			t.Errorf("Match(%q) matched %s, want no match", c.path, m.Label)
			continue
		}
		if m.Label != c.label {
			t.Errorf("Match(%q) label=%s want %s", c.path, m.Label, c.label)
		}
		if !reflect.DeepEqual(m.Params, c.params) {
			t.Errorf("Match(%q) params=%v want %v", c.path, m.Params, c.params)
		}
	}
}

func TestConflictRegistration(t *testing.T) {
	rt := New()
	if err := rt.Register("/users/:id", "a"); err != nil {
		t.Fatal(err)
	}
	// structurally identical parameter patterns conflict.
	if err := rt.Register("/users/:name", "b"); err == nil {
		t.Error("expected conflict for /users/:name")
	}
	// identical pattern conflicts.
	if err := rt.Register("/users/:id", "c"); err == nil {
		t.Error("expected conflict for duplicate /users/:id")
	}
	// non-conflicting variations register fine.
	nonConflict := []string{
		"/users/list",        // static vs parameter at position 1
		"/users/:id/profile", // longer
		"/files/*path",       // different prefix
		"/files",             // shorter than the catch-all variant
		"/admins/:id",        // different static prefix
	}
	for _, p := range nonConflict {
		if err := rt.Register(p, "ok"); err != nil {
			t.Errorf("unexpected error for %q: %v", p, err)
		}
	}
	// catch-all vs catch-all with same prefix conflicts.
	if err := rt.Register("/files/*other", "g"); err == nil {
		t.Error("expected conflict for /files/*other vs /files/*path")
	}
	// empty label rejected.
	if err := rt.Register("/x", ""); err == nil {
		t.Error("expected error for empty label")
	}
}

func TestIllegalCatchAll(t *testing.T) {
	rt := New()
	if err := rt.Register("/bad/*x/y", "bad"); err == nil {
		t.Error("expected error for catch-all not in last position")
	}
	if err := rt.Register("/ok/*tail", "ok"); err != nil {
		t.Errorf("unexpected error for trailing catch-all: %v", err)
	}
}

func TestMoreSpecific(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"/users/list", "/users/:id", true},   // static beats parameter
		{"/users/:id", "/users/list", false},
		{"/users/:id", "/users/*x", true},      // parameter beats catch-all
		{"/files", "/files/*p", true},          // shorter static beats catch-all tail
		{"/files/*p", "/files", false},
		{"/a/:x/:y", "/a/:m/*z", true},         // parameter beats catch-all at position 2
		{"/users/:id", "/users/:id", false},    // identical: not strictly more specific
	}
	for _, c := range cases {
		a, _ := parsePattern(c.a)
		b, _ := parsePattern(c.b)
		if got := moreSpecific(a, b); got != c.want {
			t.Errorf("moreSpecific(%q,%q) = %v want %v", c.a, c.b, got, c.want)
		}
	}
}
