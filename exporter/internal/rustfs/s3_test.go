package rustfs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestS3Client_ListBuckets_ParsesXML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		// 校验 Authorization 头存在
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult>
  <Buckets>
    <Bucket><Name>alpha</Name></Bucket>
    <Bucket><Name>beta</Name></Bucket>
  </Buckets>
</ListAllMyBucketsResult>`))
	}))
	defer srv.Close()

	c, err := NewS3Client(srv.URL, "us-east-1", "ak", "sk", TLSOptions{})
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}
	got, err := c.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	want := []string{"alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestS3Client_ListBuckets_PropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, err := NewS3Client(srv.URL, "us-east-1", "ak", "sk", TLSOptions{})
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}
	if _, err := c.ListBuckets(context.Background()); err == nil {
		t.Fatal("expected error on 500")
	}
}