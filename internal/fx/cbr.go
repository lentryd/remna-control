package fx

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

const cbrURL = "https://www.cbr.ru/scripts/XML_daily.asp?date_req=%s"

// CBR is the Central Bank of Russia: RUB per one unit of a currency.
type CBR struct{ http *http.Client }

func (*CBR) Name() string { return "CBR" }

type valCurs struct {
	Valutes []struct {
		CharCode string `xml:"CharCode"`
		Nominal  string `xml:"Nominal"`
		Value    string `xml:"Value"`
	} `xml:"Valute"`
}

func (c *CBR) Fetch(ctx context.Context, date time.Time) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(cbrURL, date.Format("02/01/2006")), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "remna-control/1.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch CBR rates: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CBR returned %d", resp.StatusCode)
	}
	return parseCBR(resp.Body)
}

func parseCBR(r io.Reader) (map[string]float64, error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		if strings.EqualFold(charset, "windows-1251") {
			return charmap.Windows1251.NewDecoder().Reader(input), nil
		}
		return input, nil
	}
	var vc valCurs
	if err := dec.Decode(&vc); err != nil {
		return nil, fmt.Errorf("parse CBR rates: %w", err)
	}
	rates := make(map[string]float64, len(vc.Valutes))
	for _, v := range vc.Valutes {
		value, err := strconv.ParseFloat(strings.ReplaceAll(v.Value, ",", "."), 64)
		if err != nil {
			continue
		}
		nominal, err := strconv.ParseFloat(v.Nominal, 64)
		if err != nil || nominal == 0 {
			nominal = 1
		}
		rates[v.CharCode] = value / nominal
	}
	if len(rates) == 0 {
		return nil, fmt.Errorf("CBR returned no rates")
	}
	return rates, nil
}
