package fx

import (
	"sync"
	"time"
)

// Memo is a per-request memoization layer on top of Service. It collapses
// repeated (fromCurrency, toCurrency[, date]) lookups inside a single request so
// that we never call the underlying FX provider twice for the same conversion.
// Rates are computed lazily and stored with a sync.RWMutex so the memo is safe
// for concurrent use by parallel loops.
type Memo struct {
	fx *Service

	mu   sync.RWMutex
	spot map[string]float64            // key: "from|to"
	hist map[string]map[string]float64 // outer: "from|to", inner: "YYYY-MM-DD"
}

// NewMemo creates a new FX memoization session.
func NewMemo(svc *Service) *Memo {
	return &Memo{
		fx:   svc,
		spot: make(map[string]float64),
		hist: make(map[string]map[string]float64),
	}
}

// SpotRate returns a cached spot rate, fetching once per (from, to) pair.
func (m *Memo) SpotRate(from, to string) (float64, error) {
	if from == to || from == "" || to == "" {
		return 1.0, nil
	}
	key := from + "|" + to
	m.mu.RLock()
	if r, ok := m.spot[key]; ok {
		m.mu.RUnlock()
		return r, nil
	}
	m.mu.RUnlock()
	r, err := m.fx.GetSpotRate(from, to)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	m.spot[key] = r
	m.mu.Unlock()
	return r, nil
}

// HistoricalRate returns a cached historical rate, fetching once per (from, to, date).
func (m *Memo) HistoricalRate(from, to string, date time.Time) (float64, error) {
	if from == to || from == "" || to == "" {
		return 1.0, nil
	}
	key := from + "|" + to
	ds := date.Format("2006-01-02")
	m.mu.RLock()
	if inner, ok := m.hist[key]; ok {
		if r, ok2 := inner[ds]; ok2 {
			m.mu.RUnlock()
			return r, nil
		}
	}
	m.mu.RUnlock()
	r, err := m.fx.GetRate(from, to, date)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	inner := m.hist[key]
	if inner == nil {
		inner = make(map[string]float64)
		m.hist[key] = inner
	}
	inner[ds] = r
	m.mu.Unlock()
	return r, nil
}

// ConvertSpot converts an amount at the memoized spot rate.
func (m *Memo) ConvertSpot(amount float64, from, to string) (float64, error) {
	if amount == 0 || from == to {
		return amount, nil
	}
	r, err := m.SpotRate(from, to)
	if err != nil {
		return 0, err
	}
	return amount * r, nil
}

// Convert converts an amount at the memoized historical rate.
func (m *Memo) Convert(amount float64, from, to string, date time.Time) (float64, error) {
	if amount == 0 || from == to {
		return amount, nil
	}
	r, err := m.HistoricalRate(from, to, date)
	if err != nil {
		return 0, err
	}
	return amount * r, nil
}
