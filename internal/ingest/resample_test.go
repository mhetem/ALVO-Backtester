package ingest

import (
	"testing"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

func realFiveMinute(t *testing.T) (*market.Calendar, []market.Candle) {
	t.Helper()

	calendar := testCalendar(t)
	candles, rejected := Normalize(calendar, market.TF5m, loadBars(t, "brapi_petr4_5m.json"))
	if len(rejected) != 0 {
		t.Fatalf("the committed 5m fixture should normalize cleanly, got %d rejections", len(rejected))
	}
	if len(candles) == 0 {
		t.Fatal("the committed 5m fixture normalized to nothing")
	}
	return calendar, candles
}

func TestResampleRealSessionsConservesTheSeries(t *testing.T) {
	calendar, base := realFiveMinute(t)

	var (
		wantVolume int64
		wantHigh   float64
		wantLow    float64
	)
	for i, candle := range base {
		wantVolume += candle.Volume
		if i == 0 || candle.High > wantHigh {
			wantHigh = candle.High
		}
		if i == 0 || candle.Low < wantLow {
			wantLow = candle.Low
		}
	}

	for _, tf := range []market.Timeframe{market.TF15m, market.TF30m, market.TF1h} {
		t.Run(tf.String(), func(t *testing.T) {
			got, err := market.Resample(calendar, base, tf)
			if err != nil {
				t.Fatalf("Resample: %v", err)
			}
			if len(got) == 0 {
				t.Fatal("resampled to nothing")
			}

			var (
				volume int64
				high   float64
				low    float64
			)
			for i, candle := range got {
				volume += candle.Volume
				if i == 0 || candle.High > high {
					high = candle.High
				}
				if i == 0 || candle.Low < low {
					low = candle.Low
				}
			}

			if volume != wantVolume {
				t.Errorf("resampling changed total volume: %d out, %d in", volume, wantVolume)
			}
			if high != wantHigh {
				t.Errorf("resampled high is %g, want the base high %g", high, wantHigh)
			}
			if low != wantLow {
				t.Errorf("resampled low is %g, want the base low %g", low, wantLow)
			}
			if len(got) >= len(base) {
				t.Errorf("resampling to %s produced %d candles from %d base bars, which is not a fold", tf, len(got), len(base))
			}
		})
	}
}

func TestResampleRealSessionsAlignsEveryBucketToItsSessionOpen(t *testing.T) {
	calendar, base := realFiveMinute(t)

	for _, tf := range []market.Timeframe{market.TF15m, market.TF30m, market.TF1h} {
		got, err := market.Resample(calendar, base, tf)
		if err != nil {
			t.Fatalf("%s: Resample: %v", tf, err)
		}

		width := tf.BucketWidth()
		seen := map[time.Time]bool{}

		for _, candle := range got {
			session, ok := calendar.Session(candle.TS)
			if !ok {
				t.Fatalf("%s bucket %s does not fall on a trading day", tf, candle.TS.Format(time.RFC3339))
			}
			offset := candle.TS.Sub(session.Open)
			if offset < 0 || offset%width != 0 {
				t.Errorf("%s bucket %s sits %s past the session open, which is not a multiple of %s",
					tf, candle.TS.Format(time.RFC3339), offset, width)
			}
			if seen[candle.TS] {
				t.Errorf("%s bucket %s was emitted twice", tf, candle.TS.Format(time.RFC3339))
			}
			seen[candle.TS] = true

			if candle.High < candle.Open || candle.High < candle.Close ||
				candle.Low > candle.Open || candle.Low > candle.Close || candle.High < candle.Low {
				t.Errorf("%s bucket %s is not a well-formed candle: O%g H%g L%g C%g",
					tf, candle.TS.Format(time.RFC3339), candle.Open, candle.High, candle.Low, candle.Close)
			}
		}

		for i := 1; i < len(got); i++ {
			if !got[i].TS.After(got[i-1].TS) {
				t.Fatalf("%s candles are not strictly ascending at index %d", tf, i)
			}
		}
	}
}

func TestResampleRealSessionsFoldsEachBucketFromItsOwnBars(t *testing.T) {
	calendar, base := realFiveMinute(t)

	for _, tf := range []market.Timeframe{market.TF15m, market.TF30m, market.TF1h} {
		got, err := market.Resample(calendar, base, tf)
		if err != nil {
			t.Fatalf("%s: Resample: %v", tf, err)
		}

		width := tf.BucketWidth()
		for _, bucket := range got {
			members := []market.Candle{}
			for _, candle := range base {
				if !candle.TS.Before(bucket.TS) && candle.TS.Before(bucket.TS.Add(width)) {
					members = append(members, candle)
				}
			}
			if len(members) == 0 {
				t.Fatalf("%s bucket %s was emitted with no base bars inside it", tf, bucket.TS.Format(time.RFC3339))
			}

			want := market.Candle{
				TS:    bucket.TS,
				Open:  members[0].Open,
				High:  members[0].High,
				Low:   members[0].Low,
				Close: members[len(members)-1].Close,
			}
			for _, member := range members {
				want.High = max(want.High, member.High)
				want.Low = min(want.Low, member.Low)
				want.Volume += member.Volume
			}

			if bucket.Open != want.Open || bucket.High != want.High || bucket.Low != want.Low ||
				bucket.Close != want.Close || bucket.Volume != want.Volume {
				t.Errorf("%s bucket %s folded %d bars to O%g H%g L%g C%g V%d, want O%g H%g L%g C%g V%d",
					tf, bucket.TS.Format(time.RFC3339), len(members),
					bucket.Open, bucket.High, bucket.Low, bucket.Close, bucket.Volume,
					want.Open, want.High, want.Low, want.Close, want.Volume)
			}
		}
	}
}

func TestResampleRealSessionsProducesTheExpectedBucketCounts(t *testing.T) {
	calendar, base := realFiveMinute(t)

	sessions := map[string]bool{}
	for _, candle := range base {
		session, _ := calendar.Session(candle.TS)
		sessions[session.Day.Format(time.DateOnly)] = true
	}

	for _, tc := range []struct {
		tf     market.Timeframe
		perDay int
	}{
		{market.TF15m, 28},
		{market.TF30m, 14},
		{market.TF1h, 7},
	} {
		got, err := market.Resample(calendar, base, tc.tf)
		if err != nil {
			t.Fatalf("%s: Resample: %v", tc.tf, err)
		}

		counts := map[string]int{}
		for _, candle := range got {
			session, _ := calendar.Session(candle.TS)
			counts[session.Day.Format(time.DateOnly)]++
		}

		if len(counts) != len(sessions) {
			t.Errorf("%s covers %d sessions, want %d", tc.tf, len(counts), len(sessions))
		}
		for day, count := range counts {
			if count > tc.perDay {
				t.Errorf("%s on %s produced %d buckets, more than the %d a full session holds",
					tc.tf, day, count, tc.perDay)
			}
			if count < tc.perDay-1 {
				t.Errorf("%s on %s produced only %d buckets; a real session should fill nearly all %d",
					tc.tf, day, count, tc.perDay)
			}
		}
	}
}
