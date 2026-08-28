package vaultkv_test

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	vaultkv "github.com/cloudfoundry-community/vaultkv"
)

// newTestClient builds a vaultkv.Client with its own transport, independent
// of the package-level Ginkgo suite's client and Trace (which would mask the
// drain bug under test via DumpResponse).
func newTestClient(t *testing.T, rawURL string) *vaultkv.Client {
	t.Helper()

	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("could not parse URL %q: %s", rawURL, err)
	}

	return &vaultkv.Client{
		VaultURL:  u,
		AuthToken: "t",
		Client:    &http.Client{Transport: &http.Transport{}},
	}
}

// clientGet is a thin wrapper over the simplest exported call that reaches
// doRequest with a JSON body.
func clientGet(client *vaultkv.Client, path string, output interface{}) error {
	return client.Get(path, output)
}

// A 404 must not cost the client its keep-alive connection: three
// sequential requests (200, 404, 200) arrive on one TCP connection,
// counted via ConnState on an unstarted server (assigning ConnState
// after NewServer would clobber httptest's own hook).
func TestErrorResponsesKeepConnectionAlive(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 2 {
			w.WriteHeader(404)
			w.Write([]byte(`{"errors":["not found"]}`))
			return
		}
		w.Write([]byte(`{"data":{"k":"v"}}`))
	})

	var newConns atomic.Int64
	srv := httptest.NewUnstartedServer(handler)
	base := srv.Config.ConnState
	srv.Config.ConnState = func(c net.Conn, s http.ConnState) {
		if s == http.StateNew {
			newConns.Add(1)
		}
		if base != nil {
			base(c, s)
		}
	}
	srv.Start()
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	var out map[string]interface{}
	_ = clientGet(client, "secret/one", &out)
	if err := clientGet(client, "secret/two", &out); !vaultkv.IsNotFound(err) {
		t.Fatalf("want 404 error, got %v", err)
	}
	_ = clientGet(client, "secret/three", &out)

	if got := newConns.Load(); got != 1 {
		t.Errorf("requests used %d connections, want 1", got)
	}
}

// A 503 must not be reported as success just because a follow-up
// /sys/health re-check (performed internally by parse503 to classify the
// 503) happens to find the Vault healthy again by that point. parse503
// currently returns nil in that case, which makes doRequest report success
// with output never decoded.
func TestErrorResponsesParse503NeverMasksSealed(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sys/health") {
			w.WriteHeader(200)
			w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(503)
		w.Write([]byte(`{"errors":["vault is temporarily unavailable"]}`))
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	var out map[string]interface{}
	if err := clientGet(client, "secret/foo", &out); err == nil {
		t.Fatal("want an error for a 503 response, got nil")
	}
}

// Curl must never write transport errors to stdout; it should route them to
// Trace when set, and stay silent when Trace is nil.
func TestErrorResponsesCurlWritesTransportErrorsToTrace(t *testing.T) {
	// Nothing listens here, so dialing fails immediately with a transport error.
	u, err := url.Parse("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("could not parse URL: %s", err)
	}

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("could not create pipe: %s", err)
	}
	os.Stdout = w

	var trace bytes.Buffer
	client := &vaultkv.Client{
		VaultURL:  u,
		AuthToken: "t",
		Client:    &http.Client{Transport: &http.Transport{}},
		Trace:     &trace,
	}

	_, curlErr := client.Curl("GET", "secret/foo", nil, nil)

	w.Close()
	os.Stdout = origStdout
	var captured bytes.Buffer
	_, _ = io.Copy(&captured, r)

	if curlErr == nil {
		t.Fatal("want a transport error, got nil")
	}
	if captured.Len() != 0 {
		t.Errorf("want nothing printed to stdout, got %q", captured.String())
	}
	if trace.Len() == 0 {
		t.Error("want transport error info written to Trace, got nothing")
	}
}

type closeTrackingBody struct {
	io.ReadCloser
	closed *int32
}

func (c *closeTrackingBody) Close() error {
	atomic.AddInt32(c.closed, 1)
	return c.ReadCloser.Close()
}

type trackingRoundTripper struct {
	http.RoundTripper
	closed *int32
}

func (t *trackingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.RoundTripper.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	resp.Body = &closeTrackingBody{ReadCloser: resp.Body, closed: t.closed}
	return resp, nil
}

// Health must close its response body; it currently never does, on any path.
func TestErrorResponsesHealthClosesResponseBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("could not parse URL: %s", err)
	}

	var closed int32
	client := &vaultkv.Client{
		VaultURL:  u,
		AuthToken: "t",
		Client: &http.Client{
			Transport: &trackingRoundTripper{RoundTripper: &http.Transport{}, closed: &closed},
		},
	}

	if err := client.Health(true); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if got := atomic.LoadInt32(&closed); got != 1 {
		t.Errorf("want response body closed exactly once, got %d", got)
	}
}
