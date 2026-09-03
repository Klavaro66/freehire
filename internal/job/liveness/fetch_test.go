package liveness

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchReturnsStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello posting"))
	}))
	defer srv.Close()

	status, finalURL, body, err := Fetch(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch() err = %v", err)
	}
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if body != "hello posting" {
		t.Errorf("body = %q, want %q", body, "hello posting")
	}
	if finalURL != srv.URL {
		t.Errorf("finalURL = %q, want %q", finalURL, srv.URL)
	}
}

func TestFetchReportsHTTPGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	status, _, _, err := Fetch(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch() err = %v", err)
	}
	if status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
}

func TestFetchCapturesFinalURLAfterRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/job" {
			http.Redirect(w, r, "/careers?error=true", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("generic careers page"))
	}))
	defer srv.Close()

	status, finalURL, _, err := Fetch(context.Background(), srv.Client(), srv.URL+"/job")
	if err != nil {
		t.Fatalf("Fetch() err = %v", err)
	}
	if status != 200 {
		t.Errorf("status = %d, want 200 (followed redirect)", status)
	}
	if !strings.HasSuffix(finalURL, "/careers?error=true") {
		t.Errorf("finalURL = %q, want suffix /careers?error=true", finalURL)
	}
}

func TestFetchOnUnreachableHostReturnsZeroStatus(t *testing.T) {
	// A closed server: the request fails at the transport, which must surface as a
	// not-expired signal (status 0 + error), never a death verdict.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	status, _, _, err := Fetch(context.Background(), http.DefaultClient, url)
	if err == nil {
		t.Fatal("Fetch() to a closed server must return an error")
	}
	if status != 0 {
		t.Errorf("status = %d, want 0 on transport failure", status)
	}
}
