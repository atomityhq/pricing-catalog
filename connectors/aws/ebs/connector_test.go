package aws_ebs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pricing-catalog/pkg/connector"
	"pricing-catalog/pkg/connector/testkit"
)

var _ connector.Connector = (*Connector)(nil)

func TestConnectorContract(t *testing.T) {
	data := readFixture(t)
	records := testkit.AssertConnectorContract(t, func() connector.Connector {
		return New(bytes.NewReader(data))
	})
	if len(records) != 8 {
		t.Fatalf("got %d records, want 8", len(records))
	}
}

func TestNew(t *testing.T) {
	c := New(nil)
	if c.Name() != "aws_ebs" || c.sourceURL != SourceURL || c.httpClient == nil || c.httpClient.Timeout != 30*time.Second {
		t.Fatalf("invalid connector defaults: %#v", c)
	}
}

func TestFetchDataReader(t *testing.T) {
	data := readFixture(t)
	c := New(bytes.NewReader(data))
	c.sourceURL = ":invalid URL" // A provided reader must avoid HTTP entirely.
	got, err := c.FetchData(context.Background())
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("FetchData = %q, %v", got, err)
	}
}

func TestFetchHTTP(t *testing.T) {
	data := readFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/json" {
			t.Errorf("request method/header: %s, %s", r.Method, r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer server.Close()

	c := New(nil)
	c.sourceURL = server.URL
	c.httpClient = server.Client()
	records, err := c.Fetch(context.Background())
	if err != nil || len(records) != 8 {
		t.Fatalf("Fetch = %d records, %v", len(records), err)
	}
}

func TestFetchHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"not found", `{}`, "404", http.StatusNotFound},
		{"server error", `{}`, "500", http.StatusInternalServerError},
		{"empty status", "", "204", http.StatusNoContent},
		{"broken JSON", `{"products":`, "decode AWS offer JSON", http.StatusOK},
		{"missing fields", `{}`, "unsupported format version", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			c := New(nil)
			c.sourceURL = server.URL
			c.httpClient = server.Client()
			records, err := c.Fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) || records != nil {
				t.Fatalf("Fetch = %v, %v; want error containing %q", records, err, tc.want)
			}
		})
	}
}

func TestFetchDataErrors(t *testing.T) {
	t.Run("reader failure", func(t *testing.T) {
		want := errors.New("reader failed")
		_, err := New(failingReader{want}).Fetch(context.Background())
		if !errors.Is(err, want) {
			t.Fatalf("got %v, want wrapped reader failure", err)
		}
	})
	t.Run("invalid URL", func(t *testing.T) {
		c := New(nil)
		c.sourceURL = ":bad URL"
		if _, err := c.FetchData(context.Background()); err == nil || !strings.Contains(err.Error(), "create AWS request") {
			t.Fatalf("invalid URL error: %v", err)
		}
	})
	t.Run("transport failure", func(t *testing.T) {
		want := errors.New("connection failed")
		c := New(nil)
		c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, want })
		if _, err := c.FetchData(context.Background()); !errors.Is(err, want) {
			t.Fatalf("transport error: %v", err)
		}
	})
	for _, tc := range []struct {
		name   string
		status int
		length int64
		reader io.Reader
		want   string
	}{
		{"declared oversize", http.StatusOK, maxSize + 1, strings.NewReader(""), "response body too large"},
		{"body read error", http.StatusOK, -1, failingReader{io.ErrUnexpectedEOF}, "read AWS offer"},
		{"status error", http.StatusBadGateway, -1, strings.NewReader(""), "Bad Gateway"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedBody{Reader: tc.reader}
			c := New(nil)
			c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tc.status, Status: http.StatusText(tc.status),
					ContentLength: tc.length, Body: body, Header: make(http.Header),
				}, nil
			})
			_, err := c.FetchData(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got error %v, want %q", err, tc.want)
			}
			if !body.closed {
				t.Fatal("HTTP body was not closed on failure")
			}
		})
	}
}

func TestReadBodyLimit(t *testing.T) {
	// A small limit verifies both boundaries without allocating 256 MiB in tests.
	for _, size := range []int{0, 15, 16, 17} {
		data, err := readBody(strings.NewReader(strings.Repeat("x", size)), 16)
		if size > 16 {
			if err == nil || !strings.Contains(err.Error(), "too large") || data != nil {
				t.Fatalf("size %d: data = %v, error = %v", size, data, err)
			}
		} else if err != nil || len(data) != size {
			t.Fatalf("size %d: got %d bytes, %v", size, len(data), err)
		}
	}
}

func TestFetchCancellation(t *testing.T) {
	t.Run("before read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		for _, reader := range []io.Reader{nil, failingReader{errors.New("must not read")}} {
			if _, err := New(reader).Fetch(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error: %v", err)
			}
		}
	})
	t.Run("during HTTP", func(t *testing.T) {
		started := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
		}))
		defer server.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c := New(nil)
		c.sourceURL = server.URL
		c.httpClient = server.Client()
		result := make(chan error, 1)
		go func() {
			_, err := c.Fetch(ctx)
			result <- err
		}()
		select {
		case <-started:
			cancel()
		case <-time.After(5 * time.Second):
			t.Fatal("HTTP request did not start")
		}
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Fetch did not stop after cancellation")
		}
	})
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error {
	b.closed = true
	return nil
}
