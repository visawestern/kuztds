package cron

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/egerkuzma/kuztds/internal/cost"
)

// maxRatesBytes — the ECB daily feed is a few kilobytes; anything larger is
// not that feed.
const maxRatesBytes = 1 << 20

// runRates downloads the ECB reference rates and writes <DataDir>/rates.json.
// The file is written only after the feed parsed cleanly: a failed download
// must leave yesterday's rates in place, not blank them out.
func (r *Runner) runRates(ctx context.Context, cfg Rates) (string, error) {
	if cfg.URL == "" {
		return "", errors.New("rates: no URL configured")
	}
	body, err := r.get(ctx, cfg.URL, nil, "", "", maxRatesBytes)
	if err != nil {
		return "", fmt.Errorf("rates: %w", err)
	}
	defer body.Close()
	rates, date, err := cost.ParseECB(body)
	if err != nil {
		return "", err
	}
	path := filepath.Join(r.paths.DataDir, "rates.json")
	if err := cost.SaveRates(path, rates, date); err != nil {
		return "", fmt.Errorf("rates: %w", err)
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return "", fmt.Errorf("rates: %w", err)
	}
	return fmt.Sprintf("%d currencies for %s", len(rates), dateOrUnknown(date)), nil
}

func dateOrUnknown(date string) string {
	if date == "" {
		return "an unknown date"
	}
	return date
}
