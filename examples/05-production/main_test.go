package main

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

const webOrigin = "https://app.example.com"

// serve runs the app on a real listener with the production options and returns its URL
// and a function that shuts it down and reports what the serving call returned.
func serve(t *testing.T) (string, func() error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(webOrigin)
	done := make(chan error, 1)
	go func() { done <- app.Serve(ln) }()

	stop := func() error {
		app.Stop()
		select {
		case err := <-done:
			return err
		case <-time.After(15 * time.Second):
			t.Fatal("the app did not shut down")
			return nil
		}
	}
	return "http://" + ln.Addr().String(), stop
}

// start serves the app for one test and shuts it down when the test ends.
func start(t *testing.T) string {
	t.Helper()
	url, stop := serve(t)
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("shutdown returned %v", err)
		}
	})
	return url
}

func send(t *testing.T, method, url, body string, header map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func TestSecurityHeaders(t *testing.T) {
	url := start(t)

	res := send(t, http.MethodGet, url+"/feedback", "", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /feedback = %d, want 200", res.StatusCode)
	}
	for _, name := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy"} {
		if res.Header.Get(name) == "" {
			t.Errorf("GET /feedback has no %s header", name)
		}
	}
}

func TestCORS(t *testing.T) {
	url := start(t)

	res := send(t, http.MethodOptions, url+"/feedback", "", map[string]string{
		"Origin":                        webOrigin,
		"Access-Control-Request-Method": http.MethodPost,
	})
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != webOrigin {
		t.Errorf("preflight from %s: Access-Control-Allow-Origin = %q", webOrigin, got)
	}
}

func TestBodySizeLimit(t *testing.T) {
	url := start(t)

	big := `{"message":"` + strings.Repeat("a", 20<<10) + `"}`
	if res := send(t, http.MethodPost, url+"/feedback", big, nil); res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("POST of 20 KiB = %d, want 413", res.StatusCode)
	}
}

func TestPostRateLimit(t *testing.T) {
	url := start(t)

	for i := 1; i <= 5; i++ {
		if res := send(t, http.MethodPost, url+"/feedback", `{"message":"The checkout page is slow"}`, nil); res.StatusCode != http.StatusCreated {
			t.Fatalf("POST /feedback #%d = %d, want 201", i, res.StatusCode)
		}
	}
	res := send(t, http.MethodPost, url+"/feedback", `{"message":"One too many"}`, nil)
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Errorf("POST /feedback #6 = %d, Retry-After %q; want 429 with Retry-After", res.StatusCode, res.Header.Get("Retry-After"))
	}
}

func TestGracefulShutdown(t *testing.T) {
	url, stop := serve(t)
	send(t, http.MethodGet, url+"/feedback", "", nil)
	if err := stop(); err != nil {
		t.Errorf("shutdown returned %v, want nil", err)
	}
}
