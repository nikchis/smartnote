package yadisk

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/domain"
)

func TestHrefClientSecureLink(t *testing.T) {
	c := newHrefClient(nil, nil)

	tests := []struct {
		name string
		href string
		ok   bool
	}{
		{name: "upload host with port", href: "https://uploader1.disk.yandex.net:443/upload-target/x?y=1", ok: true},
		{name: "download host", href: "https://downloader.disk.yandex.ru/disk/x", ok: true},
		{name: "storage host", href: "https://mds.storage.yandex.net/get/x", ok: true},
		{name: "upper case apex", href: "https://YANDEX.NET/x", ok: true},
		{name: "apex ru", href: "https://yandex.ru/x", ok: true},
		{name: "no dot before the suffix", href: "https://evilyandex.net/x"},
		{name: "suffix is not the end", href: "https://yandex.net.evil.com/x"},
		{name: "other zone", href: "https://yandex.com/x"},
		{name: "http", href: "http://x.yandex.ru/x"},
		{name: "userinfo hides the host", href: "https://x.yandex.net@evil.com/x"},
		{name: "trailing dot", href: "https://yandex.net./x"},
		{name: "no host", href: "https:///x"},
		{name: "IP address", href: "https://127.0.0.1/x"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.secureLink(tc.href)

			switch {
			case tc.ok && err != nil:
				t.Errorf("secureLink = %v, want accepted", err)
			case !tc.ok && !errors.Is(err, errInsecureLink):
				t.Errorf("secureLink = %v, want errInsecureLink", err)
			}

			if err != nil && strings.Contains(err.Error(), "evil") {
				t.Errorf("the error carries the link: %v", err)
			}
		})
	}
}

// stalledRequest sends a GET to a server that never answers, with hc.
func stalledRequest(ctx context.Context, t *testing.T, hc *http.Client) error {
	t.Helper()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))

	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	_, err = roundTrip(hc, req, stepStat, nil)

	return err
}

func TestRoundTripTransportTimeoutIsCloudError(t *testing.T) {
	err := stalledRequest(t.Context(), t, &http.Client{Timeout: 50 * time.Millisecond})

	ce, ok := errors.AsType[cloudhttp.CloudError](err)
	if !ok {
		t.Fatalf("err = %v, want a cloudhttp.CloudError", err)
	}

	if ce.Code != cloudhttp.CodeNetwork || ce.Net != cloudhttp.NetTimeout || ce.Step != stepStat {
		t.Errorf("cloudhttp.CloudError = %+v, want network timeout at stat", ce)
	}

	if errors.Is(err, context.DeadlineExceeded) {
		t.Error("a transport timeout matches context.DeadlineExceeded")
	}
}

func TestRoundTripContextDeadlineIsContextError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	err := stalledRequest(ctx, t, &http.Client{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}

	if _, ok := errors.AsType[cloudhttp.CloudError](err); ok {
		t.Errorf("err = %v, a context deadline became a cloudhttp.CloudError", err)
	}
}

func TestRoundTripCanceledIsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)

	if err := stalledRequest(ctx, t, &http.Client{}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestHrefGetTransportTimeoutAndContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))

	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	base := srv.Client()
	base.Timeout = 50 * time.Millisecond
	c := newHrefClient(base, func(string) bool { return true })

	_, err := c.get(t.Context(), srv.URL, 1024, nil)

	ce, ok := errors.AsType[cloudhttp.CloudError](err)
	if !ok || ce.Code != cloudhttp.CodeNetwork || ce.Net != cloudhttp.NetTimeout || ce.Step != stepDownload {
		t.Errorf("live ctx: err = %v, want a network timeout cloudhttp.CloudError at download", err)
	}

	if errors.Is(err, context.DeadlineExceeded) {
		t.Error("live ctx: a transport timeout matches context.DeadlineExceeded")
	}

	// A client without a transport timeout: only the context can end it.
	plain := srv.Client()
	plain.Timeout = 0
	c = newHrefClient(plain, func(string) bool { return true })

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err = c.get(ctx, srv.URL, 1024, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expired ctx: err = %v, want context.DeadlineExceeded", err)
	}
}

func TestYandexReasons(t *testing.T) {
	tests := []struct {
		status int
		want   cloudhttp.Reason
	}{
		{http.StatusInsufficientStorage, cloudhttp.ReasonNoSpace},
		{http.StatusRequestEntityTooLarge, cloudhttp.ReasonTooLarge},
		{http.StatusTooManyRequests, cloudhttp.ReasonNone},
		{http.StatusServiceUnavailable, cloudhttp.ReasonNone},
	}

	for _, tc := range tests {
		ce := answerError(tc.status, []byte(`{"error":"DiskInternalError"}`))
		if ce.Reason != tc.want || ce.Provider != domain.KindYandexDisk || ce.Status != tc.status {
			t.Errorf("answerError(%d) = %+v, want reason %q of yandex_disk", tc.status, ce, tc.want)
		}
	}
}
