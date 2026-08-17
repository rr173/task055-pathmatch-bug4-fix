// Command pathmatch runs the path router service.
//
// With --smoke-test it registers a representative route table, exercises
// matching across the supported segment kinds and boundary cases, then
// exits with code 0. No external services or network are used.
package main

import (
	"flag"
	"fmt"
	"os"

	"task055-pathmatch/router"
)

func main() {
	smoke := flag.Bool("smoke-test", false, "run the self-check and exit")
	flag.Parse()
	if *smoke {
		if err := runSmoke(); err != nil {
			fmt.Fprintln(os.Stderr, "smoke-test failed:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	fmt.Println("pathmatch: path router service. Use --smoke-test to run the self-check.")
}

func runSmoke() error {
	rt := router.New()

	registrations := []struct {
		pattern, label string
	}{
		{"/", "root"},
		{"/users", "users"},
		{"/users/list", "users-list"},
		{"/users/:id", "user-detail"},
		{"/users/:id/profile", "user-profile"},
		{"/files/*path", "file-serve"},
		{"/files", "files-index"},
		{"/static/*asset", "static-asset"},
	}
	fmt.Println("== register ==")
	for _, r := range registrations {
		if err := rt.Register(r.pattern, r.label); err != nil {
			return fmt.Errorf("register %s: %w", r.pattern, err)
		}
		fmt.Printf("  %-22s -> %s\n", r.pattern, r.label)
	}

	fmt.Println("== rejection cases ==")
	// structurally identical to /users/:id -> conflict.
	if err := rt.Register("/users/:name", "user-by-name"); err != nil {
		fmt.Printf("  conflict  /users/:name       : %v\n", err)
	} else {
		return fmt.Errorf("expected conflict for /users/:name")
	}
	// catch-all not in the last position -> illegal pattern.
	if err := rt.Register("/bad/*x/y", "bad"); err != nil {
		fmt.Printf("  illegal   /bad/*x/y          : %v\n", err)
	} else {
		return fmt.Errorf("expected error for catch-all not last")
	}
	// empty label -> rejected.
	if err := rt.Register("/x", ""); err != nil {
		fmt.Printf("  emptylabel /x                : %v\n", err)
	} else {
		return fmt.Errorf("expected error for empty label")
	}

	fmt.Println("== match ==")
	queries := []string{
		"/",
		"/users",
		"/users/",
		"/users/list", // static beats parameter
		"/users/42",
		"/users/42/profile",
		"/files", // shorter static beats catch-all
		"/files/a/b/c",
		"/static/css/app.css",
		"/unknown",
		"/users//x", // invalid path -> no match
		"noslash",   // invalid path -> no match
	}
	for _, q := range queries {
		m, ok := rt.Match(q)
		if !ok {
			fmt.Printf("  %-24q -> NO MATCH\n", q)
			continue
		}
		fmt.Printf("  %-24q -> %-13s params=%v\n", q, m.Label, m.Params)
	}
	return nil
}
