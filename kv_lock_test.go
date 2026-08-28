package vaultkv_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	vaultkv "github.com/cloudfoundry-community/vaultkv"
)

// mountLockBarrier parks the first `need` matching arrivals until all of
// them have shown up, then releases every one of them at once. If `need`
// arrivals never happen within timeout, the first arrival to notice releases
// itself (and any other waiter) and records that it timed out, so a
// serialized caller can never hang the test past its own deadline.
type mountLockBarrier struct {
	need    int
	timeout time.Duration

	mu       sync.Mutex
	arrived  int
	tripped  bool
	timedOut bool
	release  chan struct{}
}

func newMountLockBarrier(need int, timeout time.Duration) *mountLockBarrier {
	return &mountLockBarrier{
		need:    need,
		timeout: timeout,
		release: make(chan struct{}),
	}
}

func (b *mountLockBarrier) park() {
	b.mu.Lock()
	if !b.tripped {
		b.arrived++
		if b.arrived >= b.need {
			b.tripped = true
			close(b.release)
		}
	}
	b.mu.Unlock()

	select {
	case <-b.release:
	case <-time.After(b.timeout):
		b.mu.Lock()
		if !b.tripped {
			b.timedOut = true
			b.tripped = true
			close(b.release)
		}
		b.mu.Unlock()
	}
}

func (b *mountLockBarrier) didTimeOut() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.timedOut
}

// TestMountLock proves that mountForPath resolves mounts for two different
// cold path segments concurrently instead of serializing every caller
// behind KV.lock across the IsKVv2Mount round trip. IsKVv2Mount sends an
// identical bare GET regardless of the path being resolved (kv2.go), so
// this parks that endpoint on a 2-arrival barrier: today's exclusive lock
// means only one lookup is ever in flight, so the barrier times out. Once
// the lookup is single-flighted per path segment (rather than globally),
// both lookups -- for different segments -- run their round trips at the
// same time and the barrier releases.
func TestMountLock(t *testing.T) {
	barrier := newMountLockBarrier(2, 5*time.Second)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sys/internal/ui/mounts":
			barrier.park()
			w.Header().Set("Content-Type", "application/json")
			// No "secret" key: IsKVv2Mount falls back to mountPathDefault
			// with isV2=false rather than erroring.
			_, _ = w.Write([]byte(`{}`))

		case strings.HasPrefix(r.URL.Path, "/v1/mountA/") || strings.HasPrefix(r.URL.Path, "/v1/mountB/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"k":"v"}}`))

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":["not found"]}`))
		}
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	kv := client.NewKV()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	paths := []string{"mountA/secret1", "mountB/secret2"}

	wg.Add(2)
	for i, path := range paths {
		go func(i int, path string) {
			defer wg.Done()
			var out map[string]string
			_, errs[i] = kv.Get(path, &out, nil)
		}(i, path)
	}
	wg.Wait()

	if barrier.didTimeOut() {
		t.Fatal("mount lookups for different segments were serialized: barrier timed out waiting for both to be in flight")
	}
	for i, err := range errs {
		if err != nil {
			t.Errorf("Get(%q) returned error: %v", paths[i], err)
		}
	}
}

// runRecovered runs fn in its own goroutine, swallowing any panic it raises,
// and reports whether it returned within timeout. A single-flight leader that
// dies mid-lookup must not strand the callers parked behind it, so every wait
// here is bounded: a regression fails the test rather than hanging the suite.
func runRecovered(timeout time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }()
		fn()
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// TestMountLookupPanicDoesNotPoisonSegment proves that the single-flight
// bookkeeping is released even when the leader's mount lookup does not return
// normally. The leader registers a channel for its path's first segment and
// every later caller under that segment waits on it, so cleanup that only runs
// on the ordinary return path leaves the channel unclosed and the entry in the
// map: the segment then blocks every subsequent caller for the life of the
// process.
//
// A nil VaultURL makes Curl panic inside IsKVv2Mount; that is only the cheapest
// way to raise a panic there, and any other would poison the segment alike.
func TestMountLookupPanicDoesNotPoisonSegment(t *testing.T) {
	kv := (&vaultkv.Client{}).NewKV()

	if !runRecovered(5*time.Second, func() {
		var out map[string]string
		_, _ = kv.Get("poisoned/secret", &out, nil)
	}) {
		t.Fatal("leader never returned; expected it to panic and unwind")
	}

	if !runRecovered(5*time.Second, func() {
		var out map[string]string
		_, _ = kv.Get("poisoned/other", &out, nil)
	}) {
		t.Fatal("segment poisoned: a later caller blocked on the dead leader's channel")
	}
}
