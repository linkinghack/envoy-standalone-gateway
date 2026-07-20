package state

import (
	"errors"
	"sync"
	"time"
)

// SeriesKey identifies one numeric Envoy metric series.
type SeriesKey struct {
	Dim    string `json:"dim"`
	Name   string `json:"name"`
	Metric string `json:"metric"`
}

// Series is a fixed-size ring buffer of samples.
type Series struct {
	Key      SeriesKey     `json:"key"`
	Interval time.Duration `json:"interval"`
	Base     time.Time     `json:"base"`
	Values   []int64       `json:"values"`
	Head     int           `json:"-"`
	Filled   int           `json:"-"`
}

// SeriesStore stores bounded in-memory time series.
type SeriesStore struct {
	mu       sync.Mutex
	capacity int
	max      int
	interval time.Duration
	series   map[SeriesKey]*Series
}

// NewSeriesStore creates a bounded ring-buffer store.
func NewSeriesStore(capacity, max int, interval time.Duration) *SeriesStore {
	if capacity <= 0 {
		capacity = 2160
	}
	if max <= 0 {
		max = 5000
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &SeriesStore{capacity: capacity, max: max, interval: interval, series: map[SeriesKey]*Series{}}
}

// Add appends a sample, creating a series until the configured cardinality cap.
func (s *SeriesStore) Add(key SeriesKey, value int64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.series[key]
	if item == nil {
		if len(s.series) >= s.max {
			return errors.New("series cardinality limit reached")
		}
		item = &Series{Key: key, Interval: s.interval, Base: at, Values: make([]int64, s.capacity)}
		s.series[key] = item
	}
	item.Values[item.Head] = value
	item.Head = (item.Head + 1) % len(item.Values)
	if item.Filled < len(item.Values) {
		item.Filled++
	}
	return nil
}

// Snapshot returns a deterministic copy of all series.
func (s *SeriesStore) Snapshot() []Series {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Series, 0, len(s.series))
	for _, item := range s.series {
		copyItem := *item
		copyItem.Values = append([]int64(nil), item.Values...)
		out = append(out, copyItem)
	}
	return out
}
