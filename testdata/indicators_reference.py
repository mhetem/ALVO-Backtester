"""Regenerates testdata/indicator_bars.json and testdata/indicator_golden.json.

    python3 testdata/indicators_reference.py

See BUILD_PLAN.md, Phase 5, for what this file is for and why it is Python.
"""

import datetime
import json
import math
import pathlib

BARS = 200
HERE = pathlib.Path(__file__).parent


def load_bars():
    raw = json.loads((HERE / "brapi_petr4_1d.json").read_text())
    history = raw["results"][0]["historicalDataPrice"][-BARS:]

    bars = []
    for entry in history:
        if entry["date"] % 86400 != 10800:
            raise SystemExit(f"{entry['date']} is not local midnight in Sao Paulo")
        opened = datetime.datetime.fromtimestamp(
            entry["date"] + 10 * 3600, datetime.UTC
        )
        bars.append(
            {
                "ts": opened.strftime("%Y-%m-%dT%H:%M:%SZ"),
                "open": entry["open"],
                "high": entry["high"],
                "low": entry["low"],
                "close": entry["close"],
                "volume": entry["volume"],
            }
        )
    return bars


def source(bars, name):
    if name == "close":
        return [b["close"] for b in bars]
    if name == "open":
        return [b["open"] for b in bars]
    if name == "high":
        return [b["high"] for b in bars]
    if name == "low":
        return [b["low"] for b in bars]
    if name == "hl2":
        return [(b["high"] + b["low"]) / 2 for b in bars]
    if name == "hlc3":
        return [(b["high"] + b["low"] + b["close"]) / 3 for b in bars]
    if name == "ohlc4":
        return [(b["open"] + b["high"] + b["low"] + b["close"]) / 4 for b in bars]
    raise SystemExit(f"unknown source {name}")


def sma(values, period):
    out = []
    for i in range(len(values)):
        if i + 1 < period:
            out.append(None)
        else:
            out.append(sum(values[i - period + 1 : i + 1]) / period)
    return out


def ema(values, period):
    alpha = 2 / (period + 1)
    out = [None] * len(values)
    if len(values) < period:
        return out

    current = sum(values[:period]) / period
    out[period - 1] = current
    for i in range(period, len(values)):
        current += alpha * (values[i] - current)
        out[i] = current
    return out


def rsi(values, period):
    out = [None] * len(values)
    if len(values) <= period:
        return out

    gains = [max(values[i] - values[i - 1], 0.0) for i in range(1, len(values))]
    losses = [max(values[i - 1] - values[i], 0.0) for i in range(1, len(values))]

    avg_gain = sum(gains[:period]) / period
    avg_loss = sum(losses[:period]) / period
    out[period] = 100.0 if avg_loss == 0 else 100 - 100 / (1 + avg_gain / avg_loss)

    for i in range(period, len(gains)):
        avg_gain += (gains[i] - avg_gain) / period
        avg_loss += (losses[i] - avg_loss) / period
        out[i + 1] = 100.0 if avg_loss == 0 else 100 - 100 / (1 + avg_gain / avg_loss)

    return out


def macd(values, fast, slow, signal):
    fast_line = ema(values, fast)
    slow_line = ema(values, slow)

    line = [
        None if f is None or s is None else f - s
        for f, s in zip(fast_line, slow_line, strict=True)
    ]

    start = slow - 1
    signal_line = ema(line[start:], signal)
    signal_line = [None] * start + signal_line

    macd_out, signal_out, hist_out = [], [], []
    for value, smoothed in zip(line, signal_line, strict=True):
        if value is None or smoothed is None:
            macd_out.append(None)
            signal_out.append(None)
            hist_out.append(None)
        else:
            macd_out.append(value)
            signal_out.append(smoothed)
            hist_out.append(value - smoothed)

    return macd_out, signal_out, hist_out


def bbands(values, period, mult):
    middle = sma(values, period)

    upper, lower = [], []
    for i, mean in enumerate(middle):
        if mean is None:
            upper.append(None)
            lower.append(None)
            continue
        chunk = values[i - period + 1 : i + 1]
        variance = sum((x - mean) ** 2 for x in chunk) / period
        spread = mult * math.sqrt(variance)
        upper.append(mean + spread)
        lower.append(mean - spread)

    return upper, middle, lower


def trim(named):
    length = len(next(iter(named.values())))
    start = length
    for i in range(length):
        if all(series[i] is not None for series in named.values()):
            start = i
            break

    for name, series in named.items():
        if any(value is None for value in series[start:]):
            raise SystemExit(f"{name} has a hole after it starts emitting")

    return start, {
        name: [round(value, 6) for value in series[start:]]
        for name, series in named.items()
    }


def case(key, named):
    start, series = trim(named)
    return {"key": key, "start": start, "series": series}


def main():
    bars = load_bars()
    close = source(bars, "close")
    hl2 = source(bars, "hl2")

    macd_line, macd_signal, macd_hist = macd(close, 12, 26, 9)
    bb_upper, bb_middle, bb_lower = bbands(close, 20, 2.0)

    cases = [
        case("sma:5", {"sma": sma(close, 5)}),
        case("sma:20", {"sma": sma(close, 20)}),
        case("ema:9", {"ema": ema(close, 9)}),
        case("ema:21", {"ema": ema(close, 21)}),
        case("ema:9:source=hl2", {"ema": ema(hl2, 9)}),
        case("rsi:14", {"rsi": rsi(close, 14)}),
        case("rsi:2", {"rsi": rsi(close, 2)}),
        case(
            "macd:12:26:9",
            {"macd": macd_line, "signal": macd_signal, "histogram": macd_hist},
        ),
        case("bb:20:2", {"upper": bb_upper, "middle": bb_middle, "lower": bb_lower}),
    ]

    (HERE / "indicator_bars.json").write_text(
        json.dumps({"symbol": "PETR4", "timeframe": "1d", "candles": bars}, indent=1)
        + "\n"
    )
    (HERE / "indicator_golden.json").write_text(
        json.dumps({"bars": "indicator_bars.json", "cases": cases}, indent=1) + "\n"
    )

    print(f"{len(bars)} bars, {len(cases)} cases")


if __name__ == "__main__":
    main()
