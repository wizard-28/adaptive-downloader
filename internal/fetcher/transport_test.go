package fetcher

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

// TestDedicatedTransportIsHTTP11Only performs a real plaintext request through
// the transport built for cfg.Client == nil and asserts the response is
// HTTP/1.1 with keep-alive enabled.
func TestDedicatedTransportIsHTTP11Only(t *testing.T) {
	content := pattern(64)
	srv := newRangeServer(t, content)
	f := New(Config{Timeout: 2 * time.Second})

	resp, err := f.client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.ProtoMajor != 1 || resp.ProtoMinor != 1 {
		t.Fatalf("Proto = %d.%d, want 1.1", resp.ProtoMajor, resp.ProtoMinor)
	}
	if resp.Close {
		t.Fatal("resp.Close = true, want keep-alive enabled")
	}
}

// TestDedicatedTransportRejectsHTTP2OverTLS proves the client refuses HTTP/2
// even when the server offers it: an h2-enabled TLS test server must still see
// an HTTP/1.1 request.
func TestDedicatedTransportRejectsHTTP2OverTLS(t *testing.T) {
	serverProto := make(chan int, 1)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverProto <- r.ProtoMajor*10 + r.ProtoMinor
		w.WriteHeader(http.StatusOK)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	if !slices.Contains(srv.TLS.NextProtos, "h2") {
		t.Fatalf("test server does not offer h2 (NextProtos = %v); cannot prove client-side enforcement", srv.TLS.NextProtos)
	}

	f := New(Config{Timeout: 2 * time.Second})
	tr, ok := f.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", f.client.Transport)
	}
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}

	resp, err := f.client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.ProtoMajor != 1 || resp.ProtoMinor != 1 {
		t.Fatalf("Proto = %d.%d, want 1.1", resp.ProtoMajor, resp.ProtoMinor)
	}
	if resp.TLS == nil {
		t.Fatal("resp.TLS = nil, want TLS state for https request")
	}
	if got := resp.TLS.NegotiatedProtocol; got == "h2" {
		t.Fatal("ALPN negotiated h2, want HTTP/1.1 only")
	}
	if got := <-serverProto; got != 11 {
		t.Fatalf("server observed proto %d, want 11 (HTTP/1.1)", got)
	}
}

// TestDedicatedTransportPoolCapacity checks the transport is configured for
// HTTP/1.1 only and can retain enough idle connections for Cmax = 16 flows.
func TestDedicatedTransportPoolCapacity(t *testing.T) {
	f := New(Config{})
	tr, ok := f.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", f.client.Transport)
	}

	if tr.Protocols == nil {
		t.Fatal("Protocols = nil, want explicit HTTP/1.1-only set")
	}
	if !tr.Protocols.HTTP1() {
		t.Error("Protocols does not enable HTTP/1")
	}
	if tr.Protocols.HTTP2() {
		t.Error("Protocols enables HTTP/2")
	}
	if tr.Protocols.UnencryptedHTTP2() {
		t.Error("Protocols enables unencrypted HTTP/2")
	}

	if tr.DisableKeepAlives {
		t.Error("DisableKeepAlives = true, want keep-alive enabled")
	}
	if tr.MaxIdleConnsPerHost < 16 {
		t.Errorf("MaxIdleConnsPerHost = %d, want >= 16 (Cmax)", tr.MaxIdleConnsPerHost)
	}
	if tr.MaxIdleConns < 16 {
		t.Errorf("MaxIdleConns = %d, want >= 16 (Cmax)", tr.MaxIdleConns)
	}
}

// TestDedicatedTransportDoesNotUseOrMutateDefault verifies the nil-client path
// neither uses nor mutates http.DefaultTransport.
func TestDedicatedTransportDoesNotUseOrMutateDefault(t *testing.T) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Skipf("http.DefaultTransport is %T, not *http.Transport", http.DefaultTransport)
	}
	beforeProtocols := base.Protocols
	beforeMaxIdlePerHost := base.MaxIdleConnsPerHost
	beforeMaxIdle := base.MaxIdleConns

	f := New(Config{})
	if f.client.Transport == http.DefaultTransport {
		t.Fatal("dedicated transport must not be http.DefaultTransport")
	}
	if base.Protocols != beforeProtocols || base.MaxIdleConnsPerHost != beforeMaxIdlePerHost || base.MaxIdleConns != beforeMaxIdle {
		t.Fatalf("http.DefaultTransport mutated: Protocols=%v MaxIdleConnsPerHost=%d MaxIdleConns=%d",
			base.Protocols, base.MaxIdleConnsPerHost, base.MaxIdleConns)
	}
}

// TestExplicitClientPassedThroughUntouched verifies a caller-supplied client
// is used as-is: not replaced, not reconfigured, and cfg.Timeout does not
// modify its Timeout.
func TestExplicitClientPassedThroughUntouched(t *testing.T) {
	tr := &http.Transport{DisableKeepAlives: true}
	client := &http.Client{Transport: tr}
	f := New(Config{Client: client, Timeout: time.Nanosecond})

	if f.client != client {
		t.Fatalf("fetcher client = %p, want supplied client %p", f.client, client)
	}
	if f.client.Transport != tr {
		t.Fatalf("client transport = %T, want supplied *http.Transport", f.client.Transport)
	}
	if f.client.Timeout != 0 {
		t.Fatalf("client Timeout = %v, want untouched 0", f.client.Timeout)
	}
}
