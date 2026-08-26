package market

import (
	"math"
	"testing"
	"testing/fstest"
	"time"
)

const testRatesBody = `{
  "series": "selic-meta",
  "source": "BCB SGS 432",
  "basis": 252,
  "through": "2024-12-31",
  "rates": [
    { "from": "2022-08-04", "annual_pct": 13.75 },
    { "from": "2023-08-03", "annual_pct": 13.25 },
    { "from": "2024-05-09", "annual_pct": 10.50 }
  ]
}`

func testRates(t *testing.T) *Rates {
	t.Helper()

	fsys := fstest.MapFS{"selic.json": jsonFile(testRatesBody)}
	rates, err := LoadRates(fsys, "selic.json")
	if err != nil {
		t.Fatalf("LoadRates: %v", err)
	}
	return rates
}

func day(year int, month time.Month, date int) time.Time {
	return time.Date(year, month, date, 0, 0, 0, 0, time.UTC)
}

func TestRatesHoldEachStepUntilTheNextCopomDecision(t *testing.T) {
	rates := testRates(t)

	cases := []struct {
		day  time.Time
		want float64
	}{
		{day(2022, time.August, 4), 13.75},
		{day(2023, time.August, 2), 13.75},
		{day(2023, time.August, 3), 13.25},
		{day(2024, time.May, 8), 13.25},
		{day(2024, time.May, 9), 10.50},
		{day(2024, time.December, 31), 10.50},
	}

	for _, tc := range cases {
		if got := rates.AnnualPct(tc.day); got != tc.want {
			t.Errorf("%s: rate = %g%%, want %g%%", tc.day.Format(time.DateOnly), got, tc.want)
		}
	}
}

func TestRatesBeforeTheFirstStepUseTheFirstStep(t *testing.T) {
	rates := testRates(t)
	if got := rates.AnnualPct(day(2019, time.January, 2)); got != 13.75 {
		t.Errorf("rate = %g%%, want the first step's 13.75%%", got)
	}
}

func TestRatesKnowWhenTheyStopBeingReal(t *testing.T) {
	rates := testRates(t)

	if !rates.Covers(day(2023, time.January, 2), day(2024, time.December, 30)) {
		t.Error("the whole range sits inside the file, so it is covered")
	}
	if rates.Covers(day(2023, time.January, 2), day(2025, time.June, 30)) {
		t.Error("the range runs past through, so the curve must admit it is carrying a stale rate")
	}
	if rates.Covers(day(2021, time.January, 4), day(2024, time.January, 2)) {
		t.Error("the range starts before the first step, so it is not covered either")
	}
}

func TestPerPeriodCompoundsBackToTheAnnualRate(t *testing.T) {
	// 252 business days of the daily rate must compound to the annual figure exactly:
	// that is the Brazilian convention the file's basis declares.
	rates := testRates(t)
	at := day(2024, time.June, 3)

	daily := rates.PerPeriod(at, TradingDaysPerYear)
	if got := math.Pow(1+daily, TradingDaysPerYear) - 1; math.Abs(got-0.105) > 1e-12 {
		t.Errorf("compounded daily rate = %g, want 0.105", got)
	}

	// An hourly bar carries a seventh of a day, so seven of them make one daily step.
	hourly := rates.PerPeriod(at, TradingDaysPerYear*7)
	if got := math.Pow(1+hourly, 7) - 1; math.Abs(got-daily) > 1e-12 {
		t.Errorf("seven hourly rates compound to %g, want the daily %g", got, daily)
	}
}

func TestPerPeriodIsZeroWithoutPeriods(t *testing.T) {
	if got := testRates(t).PerPeriod(day(2024, time.June, 3), 0); got != 0 {
		t.Errorf("rate = %g, want 0 when there are no periods to spread it over", got)
	}
}

func TestBarsPerYearFollowsTheSessionLength(t *testing.T) {
	cal := testCalendar(t)

	if got := BarsPerYear(cal, TF1d); got != TradingDaysPerYear {
		t.Errorf("daily bars a year = %g, want %d", got, TradingDaysPerYear)
	}

	// The test calendar runs 10:00 to 17:00, so seven hours: 84 five-minute bars a day.
	if got := BarsPerYear(cal, TF5m); got != TradingDaysPerYear*84 {
		t.Errorf("5m bars a year = %g, want %d", got, TradingDaysPerYear*84)
	}
	if got := BarsPerYear(cal, TF1h); got != TradingDaysPerYear*7 {
		t.Errorf("1h bars a year = %g, want %d", got, TradingDaysPerYear*7)
	}
}

func TestLoadRatesRejectsBadFiles(t *testing.T) {
	cases := map[string]string{
		"no series":      `{"source":"x","basis":252,"through":"2024-12-31","rates":[{"from":"2022-08-04","annual_pct":13.75}]}`,
		"no basis":       `{"series":"s","source":"x","basis":0,"through":"2024-12-31","rates":[{"from":"2022-08-04","annual_pct":13.75}]}`,
		"bad through":    `{"series":"s","source":"x","basis":252,"through":"soon","rates":[{"from":"2022-08-04","annual_pct":13.75}]}`,
		"no rates":       `{"series":"s","source":"x","basis":252,"through":"2024-12-31","rates":[]}`,
		"bad date":       `{"series":"s","source":"x","basis":252,"through":"2024-12-31","rates":[{"from":"whenever","annual_pct":13.75}]}`,
		"negative rate":  `{"series":"s","source":"x","basis":252,"through":"2024-12-31","rates":[{"from":"2022-08-04","annual_pct":-1}]}`,
		"out of order":   `{"series":"s","source":"x","basis":252,"through":"2024-12-31","rates":[{"from":"2023-08-03","annual_pct":13.25},{"from":"2022-08-04","annual_pct":13.75}]}`,
		"through is old": `{"series":"s","source":"x","basis":252,"through":"2021-12-31","rates":[{"from":"2022-08-04","annual_pct":13.75}]}`,
		"unknown field":  `{"series":"s","source":"x","basis":252,"through":"2024-12-31","spread":1,"rates":[{"from":"2022-08-04","annual_pct":13.75}]}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{"selic.json": jsonFile(body)}
			if _, err := LoadRates(fsys, "selic.json"); err == nil {
				t.Error("LoadRates accepted the file, want an error")
			}
		})
	}
}
