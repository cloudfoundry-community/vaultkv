package vaultkv_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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
