package library

import (
	"context"
	"fmt"
	"time"

	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

const CityTimeOpDescription = `CityTimeOp: returns the current time for a supported city.
  Input:  City *string — must be "New York" or "Tokyo"; any other value is a graph execution error.
  Output: Result string — current local time formatted as RFC3339.`

var cityTimezones = map[string]string{
	"New York": "America/New_York",
	"Tokyo":    "Asia/Tokyo",
}

// CityTimeOp returns the current time in the requested city's local timezone.
// Only "New York" and "Tokyo" are supported; any other city name returns an error.
type CityTimeOp struct {
	City   *string `dag:"input"`
	Result string  `dag:"output"`
}

func (op *CityTimeOp) Setup(params *config.Params) error { return nil }
func (op *CityTimeOp) Reset() error                      { return nil }
func (op *CityTimeOp) Run(ctx context.Context) error {
	city := *op.City
	tzName, ok := cityTimezones[city]
	if !ok {
		return fmt.Errorf("CityTimeOp: unsupported city %q (supported: \"New York\", \"Tokyo\")", city)
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return fmt.Errorf("CityTimeOp: failed to load timezone %q: %w", tzName, err)
	}
	op.Result = time.Now().In(loc).Format(time.RFC3339)
	return nil
}

func init() {
	operator.RegisterOp[CityTimeOp]()
}
