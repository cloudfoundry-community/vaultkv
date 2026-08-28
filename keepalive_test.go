package vaultkv_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
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

// A response must not cost the client its keep-alive connection. What keeps
// the connection is reading the rest of the body before closing it, so that is
// what this asserts: every response body reaches EOF before Close.
//
// Counting connections instead would not do. Go 1.27's transport drains short
// bodies itself, on its own goroutine, so the connection survives there whether
// or not this library reads anything; the count keeps passing against
// unpatched code and guards nothing. Watching the body this library was handed
// holds on every toolchain.
//
// The discriminating request is the Delete. Get and List decode the body
// through a json.Decoder, which reads to EOF on its own while looking for what
// follows the top-level value, so those paths are drained whether or not
// anyone means to drain them. Set and Delete pass no output at all and decode
// nothing, which leaves their bodies entirely unread, and Vault answers both
// with a body whenever a warning rides along.
func TestErrorResponsesKeepConnectionAlive(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "DELETE":
			w.Write([]byte(`{"warnings":["nothing was there"]}`))
		case strings.HasSuffix(r.URL.Path, "/missing"):
			w.WriteHeader(404)
			w.Write([]byte(`{"errors":["not found"]}`))
		default:
			w.Write([]byte(`{"data":{"k":"v"}}`))
		}
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	tracker := &bodyTracker{}
	client := newTrackingClient(t, srv.URL, tracker)

	var out map[string]interface{}
	if err := clientGet(client, "secret/one", &out); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if err := clientGet(client, "secret/missing", &out); !vaultkv.IsNotFound(err) {
		t.Fatalf("want 404 error, got %v", err)
	}
	if err := client.Delete("secret/three"); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	closes, drained := tracker.counts()
	if closes != 3 {
		t.Fatalf("want 3 response bodies closed, got %d", closes)
	}
	if drained != 3 {
		t.Errorf("only %d of %d response bodies were read to EOF before Close; "+
			"a body closed unread costs the connection", drained, closes)
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

// Every other branch of parseError yields one of the library's own error
// types, so an unexplained 503 must too: callers switching on the error type
// otherwise fall through to their default branch and cannot tell a Vault that
// is briefly unavailable from anything else that went wrong.
func TestErrorResponsesParse503ReturnsTypedError(t *testing.T) {
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
	err := clientGet(client, "secret/foo", &out)
	if err == nil {
		t.Fatal("want an error for a 503 response, got nil")
	}
	if !vaultkv.IsTemporarilyUnavailable(err) {
		t.Fatalf("want an *vaultkv.ErrTemporarilyUnavailable, got %T: %s", err, err)
	}
	if !strings.Contains(err.Error(), "vault is temporarily unavailable") {
		t.Errorf("want Vault's own message carried through, got %q", err.Error())
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

// bodyTracker counts, across every response a transport hands back, how many
// bodies were closed and how many of those had been read to EOF by the time
// they were closed.
type bodyTracker struct {
	mu      sync.Mutex
	closes  int
	drained int
}

func (b *bodyTracker) record(sawEOF bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closes++
	if sawEOF {
		b.drained++
	}
}

func (b *bodyTracker) counts() (closes, drained int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closes, b.drained
}

// trackedBody notes whether a read ever reached EOF, and reports that to the
// tracker at the moment of Close. Recording at Close is what makes the
// assertion toolchain-independent: whatever the transport does with the
// underlying body afterwards is its own business, and is not counted here.
type trackedBody struct {
	io.ReadCloser
	tracker *bodyTracker
	sawEOF  bool
}

func (b *trackedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.sawEOF = true
	}
	return n, err
}

func (b *trackedBody) Close() error {
	b.tracker.record(b.sawEOF)
	return b.ReadCloser.Close()
}

type trackingRoundTripper struct {
	http.RoundTripper
	tracker *bodyTracker
}

func (t *trackingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.RoundTripper.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	resp.Body = &trackedBody{ReadCloser: resp.Body, tracker: t.tracker}
	return resp, nil
}

// newTrackingClient builds a vaultkv.Client whose responses are observed by
// tracker.
func newTrackingClient(t *testing.T, rawURL string, tracker *bodyTracker) *vaultkv.Client {
	t.Helper()

	client := newTestClient(t, rawURL)
	client.Client = &http.Client{
		Transport: &trackingRoundTripper{RoundTripper: &http.Transport{}, tracker: tracker},
	}
	t.Cleanup(client.Client.CloseIdleConnections)

	return client
}

// Curl must read the auth token live when installing the Vault redirect
// policy, not capture it once at installation time: a token rotated via
// SetAuthToken after the policy is installed must still reach the
// redirected hop.
func TestRedirectUsesLiveToken(t *testing.T) {
	var mu sync.Mutex
	var redirectedToken string

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/secret/first", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"k":"v"}}`))
	})
	mux.HandleFunc("/v1/secret/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/v1/secret/redirect-target", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/v1/secret/redirect-target", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		redirectedToken = r.Header.Get("X-Vault-Token")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"k":"v"}}`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	client.SetAuthToken("first")

	// Throwaway request installs today's redirect policy with "first"
	// captured.
	var out map[string]interface{}
	if err := clientGet(client, "secret/first", &out); err != nil {
		t.Fatalf("throwaway request failed: %s", err)
	}

	client.SetAuthToken("second")

	if err := clientGet(client, "secret/redirect", &out); err != nil {
		t.Fatalf("redirecting request failed: %s", err)
	}

	mu.Lock()
	got := redirectedToken
	mu.Unlock()

	if got != "second" {
		t.Errorf("redirected hop carried token %q, want %q", got, "second")
	}
}

// Installing the redirect policy on a client's first request must not race
// when multiple goroutines make that first request concurrently.
func TestRedirectConcurrentFirstRequestNoRace(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"k":"v"}}`))
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out map[string]interface{}
			if err := clientGet(client, "secret/concurrent", &out); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected error: %s", err)
	}
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

	tracker := &bodyTracker{}
	client := newTrackingClient(t, srv.URL, tracker)

	if err := client.Health(true); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if closes, _ := tracker.counts(); closes != 1 {
		t.Errorf("want response body closed exactly once, got %d", closes)
	}
}

// When Client.Client is nil -- the documented default -- Curl reaches for
// http.DefaultClient and installs the Vault redirect policy on it. That object
// is shared with everything else in the process, so the policy must not stamp
// a Vault token onto a redirect that has nothing to do with Vault.
func TestRedirectDoesNotLeakTokenToThirdParty(t *testing.T) {
	var mu sync.Mutex
	var thirdPartyToken string
	var reached bool

	thirdParty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		thirdPartyToken = r.Header.Get("X-Vault-Token")
		reached = true
		mu.Unlock()
		w.Write([]byte("ok"))
	}))
	defer thirdParty.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, thirdParty.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	vault := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"k":"v"}}`))
	}))
	defer vault.Close()

	u, err := url.Parse(vault.URL)
	if err != nil {
		t.Fatalf("could not parse URL: %s", err)
	}

	// Leaving Client.Client nil is what puts the policy on the shared
	// http.DefaultClient; put it back the way it was found.
	t.Cleanup(func() { http.DefaultClient.CheckRedirect = nil })

	client := &vaultkv.Client{VaultURL: u, AuthToken: "super-secret-vault-token"}
	var out map[string]interface{}
	if err := client.Get("secret/foo", &out); err != nil {
		t.Fatalf("request to the vault failed: %s", err)
	}

	// Traffic that has nothing to do with Vault, through the same client.
	resp, err := http.Get(redirector.URL)
	if err != nil {
		t.Fatalf("unrelated request failed: %s", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	mu.Lock()
	got, sawRequest := thirdPartyToken, reached
	mu.Unlock()

	if !sawRequest {
		t.Fatal("the third party never received the redirected request")
	}
	if got != "" {
		t.Errorf("an unrelated redirect carried X-Vault-Token %q to a third party", got)
	}
}

// A Vault standby answers with a redirect to the active node, which is a
// different host entirely. Declining to stamp the token on hops away from the
// configured Vault must not break that: Go's own redirect handling forwards
// X-Vault-Token, which it does not treat as a sensitive header, so the token
// still reaches the active node.
func TestRedirectToActiveNodeCarriesToken(t *testing.T) {
	var mu sync.Mutex
	var activeToken string

	active := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		activeToken = r.Header.Get("X-Vault-Token")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"k":"v"}}`))
	}))
	defer active.Close()

	standby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, active.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer standby.Close()

	client := newTestClient(t, standby.URL)
	client.SetAuthToken("ha-token")

	var out map[string]interface{}
	if err := clientGet(client, "secret/foo", &out); err != nil {
		t.Fatalf("redirected request failed: %s", err)
	}

	mu.Lock()
	got := activeToken
	mu.Unlock()

	if got != "ha-token" {
		t.Errorf("active node saw token %q, want %q", got, "ha-token")
	}
}

// Nothing stops two vaultkv Clients from sharing one *http.Client, and every
// Client with a nil Client.Client shares http.DefaultClient whether it means
// to or not. Their first requests must not race to install the redirect policy
// on the object they have in common.
func TestRedirectSharedHTTPClientNoRace(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"k":"v"}}`))
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("could not parse URL: %s", err)
	}

	shared := &http.Client{Transport: &http.Transport{}}
	t.Cleanup(shared.CloseIdleConnections)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		client := &vaultkv.Client{VaultURL: u, AuthToken: "t", Client: shared}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out map[string]interface{}
			if err := clientGet(client, "secret/shared", &out); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected error: %s", err)
	}
}
