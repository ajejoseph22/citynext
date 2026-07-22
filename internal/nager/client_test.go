package nager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"citynext/internal/booking"
)

func mustDate(t *testing.T, y int, m time.Month, d int) booking.Date {
	t.Helper()
	got, err := booking.NewDate(y, m, d)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	return got
}

// stubServer serves a fixed JSON body for the GB endpoint and records requests.
func stubServer(t *testing.T, body string, status int) (*httptest.Server, *atomic.Int32, *[]string) {
	t.Helper()
	var count atomic.Int32
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &count, &paths
}

const twoHolidays = `[{"date":"2075-12-25","localName":"Christmas Day","countryCode":"GB","global":true},
{"date":"2075-08-25","localName":"Summer Bank Holiday","countryCode":"GB","global":true}]`

func TestIsPublicHolidayTrueAndFalse(t *testing.T) {
	srv, _, _ := stubServer(t, twoHolidays, http.StatusOK)
	c := New(srv.URL)

	got, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 12, 25))
	if err != nil || !got {
		t.Fatalf("Christmas: got=%v err=%v, want true", got, err)
	}
	got, err = c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 8, 14))
	if err != nil || got {
		t.Fatalf("regular day: got=%v err=%v, want false", got, err)
	}
}

func TestV3URLContract(t *testing.T) {
	srv, _, paths := stubServer(t, `[]`, http.StatusOK)
	c := New(srv.URL)
	if _, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 8, 14)); err != nil {
		t.Fatalf("IsPublicHoliday: %v", err)
	}
	if len(*paths) != 1 || (*paths)[0] != "/api/v3/PublicHolidays/2075/GB" {
		t.Fatalf("request path = %v, want /api/v3/PublicHolidays/2075/GB", *paths)
	}
}

// TestRegionalHolidayBlocks proves a subdivision-scoped record (global=false,
// counties set) still blocks the date.
func TestRegionalHolidayBlocks(t *testing.T) {
	body := `[{"date":"2075-11-30","localName":"St Andrew's Day","countryCode":"GB","global":false,"counties":["GB-SCT"]}]`
	srv, _, _ := stubServer(t, body, http.StatusOK)
	c := New(srv.URL)
	got, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 11, 30))
	if err != nil || !got {
		t.Fatalf("regional holiday: got=%v err=%v, want true", got, err)
	}
}

func TestFutureYearIsSentUpstream(t *testing.T) {
	// The client must not reject a far-future year locally; it sends the request.
	srv, _, paths := stubServer(t, `[]`, http.StatusOK)
	c := New(srv.URL, WithClock(func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }))
	if _, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 8, 14)); err != nil {
		t.Fatalf("IsPublicHoliday: %v", err)
	}
	if len(*paths) != 1 || !strings.Contains((*paths)[0], "/2075/") {
		t.Fatalf("expected a 2075 request, got %v", *paths)
	}
}

func TestServerErrorFailsClosed(t *testing.T) {
	srv, _, _ := stubServer(t, `{}`, http.StatusInternalServerError)
	c := New(srv.URL)
	_, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 8, 14))
	if !errors.Is(err, booking.ErrHolidayProviderUnavailable) {
		t.Fatalf("error = %v, want ErrHolidayProviderUnavailable", err)
	}
}

func TestMalformedJSONFailsClosed(t *testing.T) {
	srv, _, _ := stubServer(t, `not json`, http.StatusOK)
	c := New(srv.URL)
	_, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 8, 14))
	if !errors.Is(err, booking.ErrHolidayProviderUnavailable) {
		t.Fatalf("error = %v, want ErrHolidayProviderUnavailable", err)
	}
}

// TestTrailingDataFailsClosed ensures a body with data after the first JSON
// value is rejected, not read as an empty (holiday-free) calendar.
func TestTrailingDataFailsClosed(t *testing.T) {
	srv, _, _ := stubServer(t, `[] {"date":"2075-12-25"}`, http.StatusOK)
	c := New(srv.URL)
	_, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 12, 25))
	if !errors.Is(err, booking.ErrHolidayProviderUnavailable) {
		t.Fatalf("error = %v, want ErrHolidayProviderUnavailable", err)
	}
}

// TestOversizeResponseFailsClosed rejects a body larger than the limit instead
// of truncating it.
func TestOversizeResponseFailsClosed(t *testing.T) {
	big := "[" + strings.Repeat(`{"date":"2075-12-25"},`, 100) + `{"date":"2075-12-26"}]`
	srv, _, _ := stubServer(t, big, http.StatusOK)
	c := New(srv.URL, WithMaxResponseBytes(16)) // far smaller than the body
	_, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 12, 25))
	if !errors.Is(err, booking.ErrHolidayProviderUnavailable) {
		t.Fatalf("error = %v, want ErrHolidayProviderUnavailable", err)
	}
}

// TestPaddingCannotHideTrailingData is the regression test for the limit bypass:
// an empty array, whitespace padding that crosses the size limit, then another
// JSON value. The client must fail closed, not cache an empty calendar.
func TestPaddingCannotHideTrailingData(t *testing.T) {
	const limit = 64
	body := "[]" + strings.Repeat(" ", limit) + `{"date":"2075-12-25"}`
	srv, _, _ := stubServer(t, body, http.StatusOK)
	c := New(srv.URL, WithMaxResponseBytes(limit))
	_, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 12, 25))
	if !errors.Is(err, booking.ErrHolidayProviderUnavailable) {
		t.Fatalf("error = %v, want ErrHolidayProviderUnavailable (padding must not hide trailing data)", err)
	}
}

func TestCacheHitAvoidsSecondCall(t *testing.T) {
	srv, count, _ := stubServer(t, twoHolidays, http.StatusOK)
	c := New(srv.URL)
	for i := 0; i < 3; i++ {
		if _, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 12, 25)); err != nil {
			t.Fatalf("IsPublicHoliday: %v", err)
		}
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 (cached)", got)
	}
}

// TestConcurrentMissesSingleFlight verifies a burst of concurrent misses for the
// same year produces exactly one upstream request.
func TestConcurrentMissesSingleFlight(t *testing.T) {
	var count atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		<-release // hold the response so all goroutines pile onto one flight
		fmt.Fprint(w, twoHolidays)
	}))
	defer srv.Close()

	c := New(srv.URL)
	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 12, 25))
		}()
	}
	time.Sleep(50 * time.Millisecond) // let goroutines reach the in-flight call
	close(release)
	wg.Wait()

	if got := count.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 (singleflight)", got)
	}
}

func TestLRUEvictionRefetches(t *testing.T) {
	srv, count, _ := stubServer(t, `[]`, http.StatusOK)
	c := New(srv.URL)
	c.maxYears = 2

	// Fill with years 2075, 2076, then 2077 evicts 2075.
	for _, y := range []int{2075, 2076, 2077} {
		if _, err := c.IsPublicHoliday(context.Background(), mustDate(t, y, 6, 2)); err != nil {
			t.Fatalf("IsPublicHoliday(%d): %v", y, err)
		}
	}
	// 2075 was evicted -> a re-query refetches (4th upstream call total).
	if _, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 6, 2)); err != nil {
		t.Fatalf("IsPublicHoliday(2075): %v", err)
	}
	if got := count.Load(); got != 4 {
		t.Fatalf("upstream calls = %d, want 4 (2075 refetched after eviction)", got)
	}
}

// TestContextCancellationReturnsPromptly asserts a caller whose context is
// cancelled stops waiting quickly (via its own select branch) instead of
// blocking on the slow upstream fetch.
func TestContextCancellationReturnsPromptly(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release // hold the response
		fmt.Fprint(w, twoHolidays)
	}))
	// LIFO: release the handler first, then Close waits on the freed handler.
	defer srv.Close()
	defer close(release)

	// Short HTTP timeout bounds the detached flight goroutine after the test.
	c := New(srv.URL, WithHTTPClient(&http.Client{Timeout: time.Second}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	start := time.Now()
	_, err := c.IsPublicHoliday(ctx, mustDate(t, 2075, 8, 14))
	if !errors.Is(err, booking.ErrHolidayProviderUnavailable) {
		t.Fatalf("error = %v, want ErrHolidayProviderUnavailable", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("caller waited %v; a cancelled caller should return promptly", elapsed)
	}
}

// TestCallerCancellationDoesNotPoisonOthers is the regression test for the
// singleflight poisoning bug. Caller A starts the shared flight and then cancels
// its context; caller B, sharing the same flight with a healthy context, must
// still receive a successful result, and only one upstream call may occur.
func TestCallerCancellationDoesNotPoisonOthers(t *testing.T) {
	var count atomic.Int32
	hit := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		select {
		case hit <- struct{}{}: // signal that the flight reached the server
		default:
		}
		<-release // hold until the test lets the shared fetch complete
		fmt.Fprint(w, twoHolidays)
	}))
	defer srv.Close()

	c := New(srv.URL)
	date := mustDate(t, 2075, 12, 25)

	// Caller A opens the flight, then will be cancelled mid-fetch.
	ctxA, cancelA := context.WithCancel(context.Background())
	go func() { _, _ = c.IsPublicHoliday(ctxA, date) }()

	select {
	case <-hit: // the flight is now in progress, blocked on release
	case <-time.After(2 * time.Second):
		t.Fatal("flight never started")
	}

	// Caller B joins the same in-flight lookup with a healthy context.
	type result struct {
		ok  bool
		err error
	}
	bDone := make(chan result, 1)
	go func() {
		ok, err := c.IsPublicHoliday(context.Background(), date)
		bDone <- result{ok, err}
	}()

	time.Sleep(50 * time.Millisecond) // let B attach to the flight
	cancelA()                         // A goes away...
	time.Sleep(50 * time.Millisecond) // ...before the fetch completes
	close(release)                    // now let the shared fetch finish

	select {
	case res := <-bDone:
		if res.err != nil {
			t.Fatalf("caller B was poisoned by A's cancellation: %v", res.err)
		}
		if !res.ok {
			t.Fatal("caller B: expected 2075-12-25 to be a holiday")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("caller B never completed")
	}

	if got := count.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 (single shared flight)", got)
	}
}

func TestCooldownSkipsRefetch(t *testing.T) {
	srv, count, _ := stubServer(t, `{}`, http.StatusInternalServerError)
	now := time.Date(2075, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(srv.URL, WithClock(func() time.Time { return now }))

	// First failure hits upstream and records the cooldown.
	if _, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 8, 14)); err == nil {
		t.Fatal("expected failure")
	}
	// Second call within cooldown must not hit upstream again.
	if _, err := c.IsPublicHoliday(context.Background(), mustDate(t, 2075, 8, 15)); err == nil {
		t.Fatal("expected cooldown failure")
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 (cooldown suppressed the retry)", got)
	}
}
