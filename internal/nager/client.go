// Package nager implements booking.HolidayProvider against the Nager.Date
// public holiday API (https://date.nager.at), using the UK (GB) calendar.
package nager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"citynext/internal/booking"
)

// defaultMaxResponseBytes caps the holiday response body. The GB calendar is
// small; a larger body is treated as an unavailable provider rather than read
// into memory.
const defaultMaxResponseBytes = 1 << 20 // 1 MiB

// defaultMaxYears bounds the number of cached year calendars, keeping memory
// flat regardless of how many distinct years are queried.
const defaultMaxYears = 16

// defaultCooldown is how long a year is not re-fetched after a failed lookup,
// so an upstream outage is not amplified into a request storm.
const defaultCooldown = 30 * time.Second

// holidaySet is the set of public-holiday dates for one year.
type holidaySet map[booking.Date]struct{}

// apiHoliday is the subset of a Nager.Date v3 record we depend on. Subdivision
// fields (global, counties) are intentionally ignored: per spec, any record
// returned by the GB endpoint for a date blocks that date.
type apiHoliday struct {
	Date string `json:"date"`
}

// Client is a caching HolidayProvider. It is safe for concurrent use.
type Client struct {
	baseURL  string
	http     *http.Client
	now      func() time.Time
	maxYears int
	maxBytes int64
	cooldown time.Duration

	group singleflight.Group

	mu     sync.Mutex
	cache  map[int]holidaySet
	order  []int             // years in LRU order, least-recent first
	failed map[int]time.Time // year -> time of last failed fetch
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the underlying HTTP client (and thus its timeout).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithClock overrides the time source (used for cooldown accounting in tests).
func WithClock(now func() time.Time) Option {
	return func(c *Client) { c.now = now }
}

// WithMaxResponseBytes overrides the response-size limit (used in tests).
func WithMaxResponseBytes(n int64) Option {
	return func(c *Client) { c.maxBytes = n }
}

// New returns a Client targeting baseURL (e.g. "https://date.nager.at").
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:  baseURL,
		http:     &http.Client{Timeout: 5 * time.Second},
		now:      time.Now,
		maxYears: defaultMaxYears,
		maxBytes: defaultMaxResponseBytes,
		cooldown: defaultCooldown,
		cache:    make(map[int]holidaySet),
		failed:   make(map[int]time.Time),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// IsPublicHoliday reports whether d is a GB public holiday.
func (c *Client) IsPublicHoliday(ctx context.Context, d booking.Date) (bool, error) {
	set, err := c.holidaysFor(ctx, d.Year())
	if err != nil {
		return false, err
	}
	_, ok := set[d]
	return ok, nil
}

// holidaysFor returns the holiday set for year, fetching and caching it on first
// use. Concurrent misses for the same year collapse into a single upstream call.
//
// The shared fetch runs on a context detached from any single caller
// (context.WithoutCancel), bounded only by the HTTP client's own timeout, so one
// caller's cancellation cannot poison the result for the others sharing the
// flight. Each caller still honours its own cancellation via the select below:
// a cancelled caller stops waiting and returns, while the flight continues to
// serve the remaining waiters.
func (c *Client) holidaysFor(ctx context.Context, year int) (holidaySet, error) {
	if set, ok := c.cached(year); ok {
		return set, nil
	}

	ch := c.group.DoChan(strconv.Itoa(year), func() (any, error) {
		// Re-check: another flight may have populated the cache meanwhile.
		if set, ok := c.cached(year); ok {
			return set, nil
		}
		if c.inCooldown(year) {
			return nil, fmt.Errorf("%w: cooling down after a recent failure for %d", booking.ErrHolidayProviderUnavailable, year)
		}
		set, err := c.fetch(context.WithoutCancel(ctx), year)
		if err != nil {
			c.recordFailure(year)
			return nil, err
		}
		c.store(year, set)
		return set, nil
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(holidaySet), nil
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %v", booking.ErrHolidayProviderUnavailable, ctx.Err())
	}
}

// fetch retrieves and parses the GB public holidays for year.
func (c *Client) fetch(ctx context.Context, year int) (holidaySet, error) {
	url := fmt.Sprintf("%s/api/v3/PublicHolidays/%d/GB", c.baseURL, year)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", booking.ErrHolidayProviderUnavailable, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", booking.ErrHolidayProviderUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: unexpected status %d from Nager", booking.ErrHolidayProviderUnavailable, resp.StatusCode)
	}

	// Read one byte past the limit so an oversized body is detected rather than
	// silently truncated. A raw io.LimitReader would produce an artificial EOF at
	// the limit, letting padding hide trailing data after a valid value.
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading response: %v", booking.ErrHolidayProviderUnavailable, err)
	}
	if int64(len(body)) > c.maxBytes {
		return nil, fmt.Errorf("%w: response exceeds %d bytes", booking.ErrHolidayProviderUnavailable, c.maxBytes)
	}

	// Decode the complete, bounded body. Requiring the second decode to reach a
	// genuine io.EOF rejects a body such as `[] {"date":"2075-12-25"}`, which
	// would otherwise decode as an empty calendar and allow a holiday booking.
	dec := json.NewDecoder(bytes.NewReader(body))
	var records []apiHoliday
	if err := dec.Decode(&records); err != nil {
		return nil, fmt.Errorf("%w: decoding response: %v", booking.ErrHolidayProviderUnavailable, err)
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: response has trailing data", booking.ErrHolidayProviderUnavailable)
	}

	set := make(holidaySet, len(records))
	for _, r := range records {
		d, err := booking.ParseDate(r.Date)
		if err != nil {
			return nil, fmt.Errorf("%w: malformed holiday date %q: %v", booking.ErrHolidayProviderUnavailable, r.Date, err)
		}
		set[d] = struct{}{}
	}
	return set, nil
}

// cached returns the cached set for year, marking it most-recently-used.
func (c *Client) cached(year int) (holidaySet, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	set, ok := c.cache[year]
	if ok {
		c.touch(year)
	}
	return set, ok
}

// store caches set for year, evicting the least-recently-used year if the cache
// is full, and clears any failure record.
func (c *Client) store(year int, set holidaySet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.failed, year)
	if _, exists := c.cache[year]; !exists && len(c.cache) >= c.maxYears {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.cache, oldest)
	}
	c.cache[year] = set
	c.touch(year)
}

// touch moves year to the most-recently-used end of the order slice. Callers
// must hold c.mu.
func (c *Client) touch(year int) {
	for i, y := range c.order {
		if y == year {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, year)
}

// inCooldown reports whether year failed recently enough to skip a re-fetch.
func (c *Client) inCooldown(year int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	last, ok := c.failed[year]
	return ok && c.now().Sub(last) < c.cooldown
}

// recordFailure timestamps a failed fetch for year.
func (c *Client) recordFailure(year int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failed[year] = c.now()
}
