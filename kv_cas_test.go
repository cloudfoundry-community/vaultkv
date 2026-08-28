package vaultkv_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	vaultkv "github.com/cloudfoundry-community/vaultkv"
)

func uintPtr(u uint) *uint {
	return &u
}

// casCaptureServer answers the mount table with a single KV v2 mount at
// "secret/" and records the raw body of every write to it, so a test can
// assert exactly what KV.Set put on the wire.
type casCaptureServer struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (c *casCaptureServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/sys/internal/ui/mounts":
			_, _ = w.Write([]byte(`{"data":{"secret":{"secret/":{"type":"kv","options":{"version":"2"}}}}}`))

		case r.Method == "PUT" && r.URL.Path == "/v1/secret/data/thing":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"errors":["could not read body"]}`))
				return
			}
			c.mu.Lock()
			c.bodies = append(c.bodies, body)
			c.mu.Unlock()
			_, _ = w.Write([]byte(`{"data":{"created_time":"2024-01-01T00:00:00.000000Z","deletion_time":"","destroyed":false,"version":5}}`))

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":["not found"]}`))
		}
	})
}

func (c *casCaptureServer) body(i int) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bodies[i]
}

// TestKVSetCASOnTheWire proves that KVSetOpts.CAS reaches Vault as the
// "cas" option of a KV v2 write, that zero is a meaningful value there (it
// means "only if the key does not exist yet", so it must not be dropped as
// a Go zero value), and that leaving CAS unset produces a request
// byte-identical to passing no options at all.
func TestKVSetCASOnTheWire(t *testing.T) {
	capture := &casCaptureServer{}
	srv := httptest.NewServer(capture.handler())
	defer srv.Close()

	kv := newTestClient(t, srv.URL).NewKV()
	values := map[string]string{"foo": "bar"}

	set := func(opts *vaultkv.KVSetOpts) vaultkv.KVVersion {
		t.Helper()
		meta, err := kv.Set("secret/thing", values, opts)
		if err != nil {
			t.Fatalf("Set with opts %+v returned error: %s", opts, err)
		}
		return meta
	}

	meta := set(nil)
	if meta.Version != 5 {
		t.Errorf("want the server's version 5 surfaced from Set, got %d", meta.Version)
	}
	set(&vaultkv.KVSetOpts{})
	set(&vaultkv.KVSetOpts{CAS: uintPtr(0)})
	set(&vaultkv.KVSetOpts{CAS: uintPtr(3)})

	type writeBody struct {
		Options map[string]json.RawMessage `json:"options"`
	}

	optionsOf := func(i int) map[string]json.RawMessage {
		t.Helper()
		var parsed writeBody
		if err := json.Unmarshal(capture.body(i), &parsed); err != nil {
			t.Fatalf("could not parse captured write body %d: %s", i, err)
		}
		return parsed.Options
	}

	if opts := optionsOf(0); opts != nil {
		t.Errorf("nil opts must produce no options block, got %v", opts)
	}
	if withEmpty, without := capture.body(1), capture.body(0); string(withEmpty) != string(without) {
		t.Errorf("empty KVSetOpts must write the same body as nil opts:\n  nil opts:   %s\n  empty opts: %s", without, withEmpty)
	}
	for i, want := range map[int]string{2: "0", 3: "3"} {
		opts := optionsOf(i)
		if opts == nil {
			t.Errorf("write %d: CAS was set but no options block was sent", i)
			continue
		}
		if got := string(opts["cas"]); got != want {
			t.Errorf("write %d: want options.cas %s, got %q", i, want, got)
		}
	}
}

// TestIsCASConflict proves that IsCASConflict recognizes Vault's
// check-and-set rejection and nothing else. Vault answers a CAS mismatch
// with a plain 400 -- not a 412 -- so the message inside the typed error is
// the only signal, and other 400s must not be mistaken for it.
func TestIsCASConflict(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/sys/internal/ui/mounts":
			_, _ = w.Write([]byte(`{"data":{"secret":{"secret/":{"type":"kv","options":{"version":"2"}}}}}`))

		case r.URL.Path == "/v1/secret/data/contested":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":["check-and-set parameter did not match the current version"]}`))

		case r.URL.Path == "/v1/secret/data/malformed":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":["did not receive HCL or JSON body"]}`))

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":["not found"]}`))
		}
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	kv := newTestClient(t, srv.URL).NewKV()
	values := map[string]string{"foo": "bar"}

	_, casErr := kv.Set("secret/contested", values, &vaultkv.KVSetOpts{CAS: uintPtr(2)})
	if casErr == nil {
		t.Fatal("want an error from the CAS-rejected write")
	}
	if !vaultkv.IsCASConflict(casErr) {
		t.Errorf("want IsCASConflict true for Vault's check-and-set rejection, got false for: %s", casErr)
	}
	if wrapped := fmt.Errorf("writing the secret: %w", casErr); !vaultkv.IsCASConflict(wrapped) {
		t.Errorf("want IsCASConflict true for a wrapped rejection, got false for: %s", wrapped)
	}

	_, otherErr := kv.Set("secret/malformed", values, nil)
	if !vaultkv.IsBadRequest(otherErr) {
		t.Fatalf("want a 400 from the malformed write, got: %v", otherErr)
	}
	if vaultkv.IsCASConflict(otherErr) {
		t.Errorf("want IsCASConflict false for a 400 that is not a CAS rejection: %s", otherErr)
	}

	_, notFoundErr := kv.Set("secret/nope/deeper", values, nil)
	if notFoundErr == nil {
		t.Fatal("want an error from the missing path")
	}
	if vaultkv.IsCASConflict(notFoundErr) {
		t.Errorf("want IsCASConflict false for a non-400 error: %s", notFoundErr)
	}

	if vaultkv.IsCASConflict(nil) {
		t.Error("want IsCASConflict false for nil")
	}
}
