package router

import (
	"sync"
	"testing"
)

// TestProbe_ConcurrentMatchNoMapPanic verifies that concurrent Match
// calls do not trigger a fatal "concurrent map writes" error.
func TestProbe_ConcurrentMatchNoMapPanic(t *testing.T) {
	rt := New()
	must := func(p, l string) {
		t.Helper()
		if err := rt.Register(p, l); err != nil {
			t.Fatalf("register %q: %v", p, err)
		}
	}
	must("/users/:id", "user")
	must("/files/*p", "files")
	must("/static/*asset", "static")

	paths := []string{"/users/1", "/users/2", "/files/a/b", "/files/c", "/static/x", "/users/3"}
	const workers = 128
	const rounds = 4
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			for r := 0; r < rounds; r++ {
				rt.Match(paths[(i+r)%len(paths)])
			}
		}(i)
	}
	close(start)
	wg.Wait()
}
