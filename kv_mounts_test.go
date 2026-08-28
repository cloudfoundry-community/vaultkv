package vaultkv_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestMountTableCachedForAllMounts proves that one fetch of the mount table
// is enough to resolve every mount it names. The table endpoint returns all
// mounts visible to the token, so a lookup for one path already holds the
// answer for every other mount; caching only the mount asked about costs
// one table round trip per distinct top-level path segment.
//
// The default 404 arm rejects any request of the wrong shape, so the two
// successful Gets also prove that each mount kept its own KV version when
// cached in bulk: a v1-shaped read against mount-a or a v2-shaped read
// against mount-b would fail the test.
func TestMountTableCachedForAllMounts(t *testing.T) {
	var mountTableFetches int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/sys/internal/ui/mounts":
			atomic.AddInt32(&mountTableFetches, 1)
			_, _ = w.Write([]byte(`{"data":{"secret":{` +
				`"mount-a/":{"type":"kv","options":{"version":"2"}},` +
				`"mount-b/":{"type":"kv","options":{"version":"1"}}}}}`))

		case r.URL.Path == "/v1/mount-a/data/first":
			_, _ = w.Write([]byte(`{"data":{"metadata":{"created_time":"2024-01-01T00:00:00.000000Z","deletion_time":"","destroyed":false,"version":1},"data":{"k":"v"}}}`))

		case r.URL.Path == "/v1/mount-b/second":
			_, _ = w.Write([]byte(`{"data":{"k":"v"}}`))

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":["not found"]}`))
		}
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	kv := newTestClient(t, srv.URL).NewKV()

	for _, path := range []string{"mount-a/first", "mount-b/second"} {
		out := map[string]string{}
		if _, err := kv.Get(path, &out, nil); err != nil {
			t.Fatalf("Get(%q) returned error: %s", path, err)
		}
		if out["k"] != "v" {
			t.Errorf("Get(%q) returned %v, want the served secret", path, out)
		}
	}

	if n := atomic.LoadInt32(&mountTableFetches); n != 1 {
		t.Errorf("want 1 mount table fetch to serve both mounts, got %d", n)
	}
}
