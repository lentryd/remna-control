package fx

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	ecbDailyURL = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"
	ecb90dURL   = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-hist-90d.xml"
	ecbHistURL  = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-hist.xml"
)

// ECB is the European Central Bank's euro reference rates, crossed through
// EUR to any base it publishes (USD, GBP, CHF, …).
type ECB struct {
	http *http.Client
	base string
}

func (*ECB) Name() string { return "ECB" }

func (e *ECB) Fetch(ctx context.Context, date time.Time) (map[string]float64, error) {
	url := ecbHistURL
	switch age := time.Since(date); {
	case age < 24*time.Hour:
		url = ecbDailyURL
	case age < 85*24*time.Hour:
		url = ecb90dURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "remna-control/1.0")
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch ECB rates: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ECB returned %d", resp.StatusCode)
	}
	perEUR, err := parseECB(resp.Body, date)
	if err != nil {
		return nil, err
	}
	return crossRates(perEUR, e.base)
}

type ecbEnvelope struct {
	Days []struct {
		Time  string `xml:"time,attr"`
		Rates []struct {
			Currency string `xml:"currency,attr"`
			Rate     string `xml:"rate,attr"`
		} `xml:"Cube"`
	} `xml:"Cube>Cube"`
}

// parseECB returns units per one EUR on the latest published day not after
// date (ECB skips weekends and TARGET holidays).
func parseECB(r io.Reader, date time.Time) (map[string]float64, error) {
	var env ecbEnvelope
	if err := xml.NewDecoder(r).Decode(&env); err != nil {
		return nil, fmt.Errorf("parse ECB rates: %w", err)
	}
	want := date.Format("2006-01-02")
	best := -1
	for i, d := range env.Days {
		if d.Time <= want && (best < 0 || d.Time > env.Days[best].Time) {
			best = i
		}
	}
	if best < 0 {
		return nil, fmt.Errorf("ECB has no rates on or before %s", want)
	}
	out := map[string]float64{"EUR": 1}
	for _, r := range env.Days[best].Rates {
		if v, err := strconv.ParseFloat(r.Rate, 64); err == nil && v > 0 {
			out[r.Currency] = v
		}
	}
	return out, nil
}

// crossRates turns units-per-EUR into base per one unit of each currency.
func crossRates(perEUR map[string]float64, base string) (map[string]float64, error) {
	b, ok := perEUR[base]
	if !ok {
		return nil, fmt.Errorf("ECB doesn't publish %s; choose another base currency", base)
	}
	out := make(map[string]float64, len(perEUR))
	for cur, v := range perEUR {
		out[cur] = b / v
	}
	return out, nil
}
