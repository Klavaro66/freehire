package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/strelov1/freehire/internal/autoapply"
)

func TestHTTPSidecarClient_Applied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req sidecarSubmitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.JobURL != "https://job-boards.greenhouse.io/acme/jobs/1" || req.Provider != "greenhouse" {
			t.Errorf("request = %+v, unexpected", req)
		}
		if req.Answers["full_name"] != "Ada Lovelace" {
			t.Errorf("answers = %+v, want full_name carried through", req.Answers)
		}
		_ = json.NewEncoder(w).Encode(sidecarSubmitResponse{Status: "applied"})
	}))
	defer srv.Close()

	c := &httpSidecarClient{baseURL: srv.URL, client: srv.Client()}
	result, err := c.Submit(context.Background(), "https://job-boards.greenhouse.io/acme/jobs/1", "greenhouse", map[string]string{"full_name": "Ada Lovelace"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != autoapply.StatusApplied {
		t.Errorf("status = %q, want %q", result.Status, autoapply.StatusApplied)
	}
}

func TestHTTPSidecarClient_Parked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sidecarSubmitResponse{
			Status: "parked",
			Reason: "1 required question unanswered",
			Unmapped: []sidecarUnmappedField{
				{ID: "question_1", Label: "Why us?", Required: true, Reason: "no known answer"},
			},
		})
	}))
	defer srv.Close()

	c := &httpSidecarClient{baseURL: srv.URL, client: srv.Client()}
	result, err := c.Submit(context.Background(), "https://x", "lever", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != autoapply.StatusParked {
		t.Errorf("status = %q, want %q", result.Status, autoapply.StatusParked)
	}
	if len(result.Unmapped) != 1 || result.Unmapped[0].ID != "question_1" {
		t.Errorf("unmapped = %+v, want the one field carried through", result.Unmapped)
	}
	if result.Reason != "1 required question unanswered" {
		t.Errorf("reason = %q", result.Reason)
	}
}

func TestHTTPSidecarClient_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	c := &httpSidecarClient{baseURL: srv.URL, client: srv.Client()}
	if _, err := c.Submit(context.Background(), "https://x", "greenhouse", nil); err == nil {
		t.Fatal("want an error on a non-2xx response")
	}
}

func TestHTTPSidecarClient_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(sidecarSubmitResponse{Status: "applied"})
	}))
	defer srv.Close()

	c := &httpSidecarClient{baseURL: srv.URL, client: srv.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := c.Submit(ctx, "https://x", "greenhouse", nil); err == nil {
		t.Fatal("want an error when the call exceeds its deadline")
	}
}
