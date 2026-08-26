package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/config"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const candleRowLimit = 40

func runCandles(ctx context.Context, cfg config.Config, log *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("candles", flag.ContinueOnError)
	ticker := flags.String("symbol", "", "ticker to read")
	timeframe := flags.String("timeframe", string(market.TF1h), "timeframe to read (5m, 15m, 30m, 1h or 1d)")
	fromText := flags.String("from", "", "first day to read, YYYY-MM-DD (default: the last stored session)")
	toText := flags.String("to", "", "last day to read, YYYY-MM-DD (default: today)")
	all := flags.Bool("all", false, "print every candle instead of the first and last few")

	if err := parseFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	tf, err := market.ParseTimeframe(*timeframe)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	pool, ingester, err := openIngester(runCtx, cfg, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	calendar := ingester.Calendar()

	symbols, err := resolveSymbols(runCtx, ingester, *ticker, false)
	if err != nil {
		return err
	}
	symbol := symbols[0]

	to := calendar.Date(time.Now().In(calendar.Location()).Date())
	if *toText != "" {
		if to, err = parseDay(calendar, *toText); err != nil {
			return err
		}
	}

	from := to
	if *fromText != "" {
		if from, err = parseDay(calendar, *fromText); err != nil {
			return err
		}
	} else if latest, ok, err := ingester.LatestStored(runCtx, symbol.ID, tf.Base()); err != nil {
		return err
	} else if ok {
		from = calendar.Date(latest.In(calendar.Location()).Date())
		to = from
	}

	series, err := market.NewCandleService(pool, calendar).Load(runCtx, symbol.ID, tf, from, to, 0)
	if err != nil {
		return err
	}

	printSeries(os.Stdout, symbol, series, from, to, *all)
	return nil
}

func printSeries(w io.Writer, symbol database.Symbol, series market.Series, from, to time.Time, all bool) {
	fmt.Fprintf(w, "%s  %s  %s..%s\n\n", symbol.Ticker, series.Timeframe, from.Format(time.DateOnly), to.Format(time.DateOnly))

	if len(series.Candles) == 0 {
		fmt.Fprintf(w, "no %s candles stored for this window\n", series.Base)
		return
	}

	fmt.Fprintf(w, "%-22s %10s %10s %10s %10s %14s\n", "ts", "open", "high", "low", "close", "volume")
	for i, candle := range series.Candles {
		if !all && len(series.Candles) > candleRowLimit && i == candleRowLimit/2 {
			fmt.Fprintf(w, "%-22s %d more candles, pass --all to print them\n", "...", len(series.Candles)-candleRowLimit)
		}
		if !all && len(series.Candles) > candleRowLimit && i >= candleRowLimit/2 && i < len(series.Candles)-candleRowLimit/2 {
			continue
		}
		fmt.Fprintf(w, "%-22s %10.2f %10.2f %10.2f %10.2f %14d\n",
			candle.TS.Format(time.RFC3339), candle.Open, candle.High, candle.Low, candle.Close, candle.Volume)
	}

	if series.Timeframe == series.Base {
		return
	}

	printReconciliation(w, series)
}

func printReconciliation(w io.Writer, series market.Series) {
	var (
		volume int64
		high   float64
		low    float64
	)
	for i, candle := range series.Candles {
		volume += candle.Volume
		if i == 0 || candle.High > high {
			high = candle.High
		}
		if i == 0 || candle.Low < low {
			low = candle.Low
		}
	}

	fmt.Fprintf(w, "\nresampled %d %s bars into %d %s candles\n",
		series.BaseBars, series.Base, len(series.Candles), series.Timeframe)
	fmt.Fprintf(w, "  volume  %14d in  %14d out   %s\n", series.BaseVolume, volume, verdict(series.BaseVolume == volume))
	fmt.Fprintf(w, "  high    %14.2f in  %14.2f out   %s\n", series.BaseHigh, high, verdict(series.BaseHigh == high))
	fmt.Fprintf(w, "  low     %14.2f in  %14.2f out   %s\n", series.BaseLow, low, verdict(series.BaseLow == low))
}

func verdict(ok bool) string {
	if ok {
		return "ok"
	}
	return "MISMATCH"
}
