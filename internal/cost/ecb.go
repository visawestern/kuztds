// ECB reference rates (G-121): the daily XML the European Central Bank
// publishes, converted into "USD per one unit" — the form the price model
// needs. The feed quotes everything against EUR, so USD itself is the bridge:
// 1 EUR = USD_rate USD, hence 1 unit = USD_rate / unit_rate USD.
package cost

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/egerkuzma/kuztds/internal/atomicfile"
)

// ratesFile is what cron writes and the engine reads.
type ratesFile struct {
	Source string             `json:"source"`
	Date   string             `json:"date"`
	Rates  map[string]float64 `json:"rates"`
}

const ratesSource = "ECB"

type ecbEnvelope struct {
	Cubes []ecbCube `xml:"Cube"`
}

// ecbCube nests: the outer Cube holds a dated Cube, which holds one Cube per
// currency. Walking it recursively keeps the parser indifferent to how many
// wrapper levels the feed uses.
type ecbCube struct {
	Time     string    `xml:"time,attr"`
	Currency string    `xml:"currency,attr"`
	Rate     string    `xml:"rate,attr"`
	Cubes    []ecbCube `xml:"Cube"`
}

// ParseECB reads the ECB daily XML and returns rates as USD per one unit
// (including USD itself = 1) plus the feed's date. A feed without USD cannot
// be rebased and is rejected: guessing there would misprice every click.
func ParseECB(r io.Reader) (map[string]float64, string, error) {
	var env ecbEnvelope
	if err := xml.NewDecoder(r).Decode(&env); err != nil {
		return nil, "", fmt.Errorf("ecb: %w", err)
	}
	var date string
	perEUR := map[string]float64{}
	var walk func(c ecbCube) error
	walk = func(c ecbCube) error {
		if c.Time != "" && date == "" {
			date = c.Time
		}
		if c.Currency != "" {
			v, err := parseRate(c.Rate)
			if err != nil {
				return fmt.Errorf("ecb: %s: %w", c.Currency, err)
			}
			perEUR[c.Currency] = v
		}
		for _, child := range c.Cubes {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, c := range env.Cubes {
		if err := walk(c); err != nil {
			return nil, "", err
		}
	}
	if len(perEUR) == 0 {
		return nil, "", errors.New("ecb: the feed has no rates")
	}
	usd, ok := perEUR["USD"]
	if !ok || usd <= 0 {
		return nil, "", errors.New("ecb: the feed has no usable USD rate")
	}
	out := make(map[string]float64, len(perEUR)+2)
	for cur, v := range perEUR {
		out[cur] = usd / v
	}
	// EUR is the feed's base, not one of its rows: 1 EUR = USD_rate USD.
	out["EUR"] = usd
	out["USD"] = 1
	return out, date, nil
}

func parseRate(s string) (float64, error) {
	var v float64
	if _, err := fmt.Sscanf(s, "%f", &v); err != nil {
		return 0, fmt.Errorf("rate %q is not a number", s)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, fmt.Errorf("rate %q must be positive", s)
	}
	return v, nil
}

// SaveRates writes the rates file atomically. Readers are the engine's hot
// path, so a torn file would be worse than yesterday's rates.
func SaveRates(path string, rates map[string]float64, date string) error {
	if rates["USD"] != 1 {
		return errors.New("cost: the USD rate must be exactly 1")
	}
	b, err := json.MarshalIndent(ratesFile{Source: ratesSource, Date: date, Rates: rates}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(b, '\n'), 0o644)
}

// LoadRates reads the rates file. A missing file is not an error: a fresh
// installation simply has no rates until the cron job runs once.
func LoadRates(path string) (map[string]float64, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", nil
		}
		return nil, "", err
	}
	var f ratesFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, "", fmt.Errorf("rates file: %w", err)
	}
	if f.Rates["USD"] != 1 {
		return nil, "", errors.New("rates file: USD must be 1")
	}
	return f.Rates, f.Date, nil
}
