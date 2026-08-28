package vaultkv_test

import (
	"bufio"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	vaultkv "github.com/cloudfoundry-community/vaultkv"
)

// stalledBodyServer answers every request with a complete set of response
// headers and a chunked body that stops one chunk in: the terminating
// zero-length chunk never arrives and the connection stays open. That models a
// Vault that hangs, a proxy that dies mid-response, or a partition that lands
// after the headers are already on the wire.
//
// The response is a 404 carrying a complete JSON error value, so the client's
// own decode finishes and any read past it is the client reaching for bytes
// the server is never going to send.
func stalledBodyServer(t *testing.T) *url.URL {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %s", err)
	}

	var mu sync.Mutex
	var conns []net.Conn
	done := make(chan struct{})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()

			go func(conn net.Conn) {
				if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
					return
				}
				_, _ = conn.Write([]byte("HTTP/1.1 404 Not Found\r\n" +
					"Content-Type: application/json\r\n" +
					"Transfer-Encoding: chunked\r\n" +
					"\r\n" +
					"18\r\n" +
					`{"errors":["not found"]}` + "\r\n"))
				// Hold the body open until the test is over.
				<-done
			}(conn)
		}
	}()

	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
		mu.Lock()
		for _, conn := range conns {
			_ = conn.Close()
		}
		mu.Unlock()
	})

	u, err := url.Parse("http://" + ln.Addr().String())
	if err != nil {
		t.Fatalf("could not parse listener URL: %s", err)
	}

	return u
}

// runBounded runs fn in its own goroutine and reports whether it returned
// within timeout. A caller stuck reading a body the server never finishes must
// fail the test, not hang the suite, so nothing here waits forever.
func runBounded(timeout time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func stalledBodyClient(t *testing.T) *vaultkv.Client {
	t.Helper()

	client := &vaultkv.Client{
		VaultURL:  stalledBodyServer(t),
		AuthToken: "t",
		Client:    &http.Client{Transport: &http.Transport{}},
	}
	t.Cleanup(client.Client.CloseIdleConnections)

	return client
}

// Draining a response body to keep the connection alive is a blocking read on
// the caller's goroutine. Curl builds its requests without a context and the
// client sets no timeout, so a body the server declares but never finishes
// must not be drained: doRequest has to give up the connection rather than the
// caller's goroutine.
func TestDrainDoesNotHangOnStalledBody(t *testing.T) {
	client := stalledBodyClient(t)

	var err error
	if !runBounded(5*time.Second, func() {
		var out map[string]interface{}
		err = client.Get("secret/foo", &out)
	}) {
		t.Fatal("Get blocked on a body the server never finished sending")
	}

	if !vaultkv.IsNotFound(err) {
		t.Errorf("want a 404 error, got %v", err)
	}
}

// Health drains its own body rather than going through doRequest, so it needs
// the same bound.
func TestDrainHealthDoesNotHangOnStalledBody(t *testing.T) {
	client := stalledBodyClient(t)

	var err error
	if !runBounded(5*time.Second, func() {
		err = client.Health(true)
	}) {
		t.Fatal("Health blocked on a body the server never finished sending")
	}

	if err == nil {
		t.Error("want an error for a 404 health response, got nil")
	}
}
