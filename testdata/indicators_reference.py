"""Regenerates testdata/indicator_bars.json and testdata/indicator_golden.json.

    python3 testdata/indicators_reference.py

See BUILD_PLAN.md, Phase 5 and Phase 6, for what this file is for and why it is Python.
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


def typical(bars):
    return [(b["high"] + b["low"] + b["close"]) / 3 for b in bars]


def first_value(series):
    for i, value in enumerate(series):
        if value is not None:
            return i
    return len(series)


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


def wilder(values, period):
    out = [None] * len(values)
    start = first_value(values)
    if start + period > len(values):
        return out

    current = sum(values[start : start + period]) / period
    out[start + period - 1] = current
    for i in range(start + period, len(values)):
        current += (values[i] - current) / period
        out[i] = current
    return out


def wma(values, period):
    weight = period * (period + 1) / 2
    out = [None] * len(values)
    for i in range(period - 1, len(values)):
        total = 0.0
        for k in range(period):
            total += (k + 1) * values[i - period + 1 + k]
        out[i] = total / weight
    return out


def hma(values, period):
    half = max(period // 2, 1)
    root = max(round(math.sqrt(period)), 1)

    fast = wma(values, half)
    slow = wma(values, period)
    raw = [
        None if f is None or s is None else 2 * f - s
        for f, s in zip(fast, slow, strict=True)
    ]

    start = period - 1
    return [None] * start + wma(raw[start:], root)


def dema(values, period):
    first = ema(values, period)
    start = period - 1
    second = [None] * start + ema(first[start:], period)
    return [
        None if a is None or b is None else 2 * a - b
        for a, b in zip(first, second, strict=True)
    ]


def tema(values, period):
    first = ema(values, period)
    start = period - 1
    second = [None] * start + ema(first[start:], period)
    start = 2 * (period - 1)
    third = [None] * start + ema(second[start:], period)
    return [
        None if a is None or b is None or c is None else 3 * a - 3 * b + c
        for a, b, c in zip(first, second, third, strict=True)
    ]


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


def stddev(values, period):
    out = [None] * len(values)
    for i in range(period - 1, len(values)):
        chunk = values[i - period + 1 : i + 1]
        mean = sum(chunk) / period
        out[i] = math.sqrt(sum((x - mean) ** 2 for x in chunk) / period)
    return out


def historical_volatility(values, period, annual):
    returns = [None] * len(values)
    for i in range(1, len(values)):
        if values[i] > 0 and values[i - 1] > 0:
            returns[i] = math.log(values[i] / values[i - 1])
        else:
            returns[i] = 0.0

    out = [None] * len(values)
    for i in range(period, len(values)):
        chunk = returns[i - period + 1 : i + 1]
        mean = sum(chunk) / period
        spread = math.sqrt(sum((x - mean) ** 2 for x in chunk) / period)
        out[i] = 100 * spread * math.sqrt(annual)
    return out


def true_range(bars):
    out = [None] * len(bars)
    for i in range(1, len(bars)):
        prev = bars[i - 1]["close"]
        out[i] = max(
            bars[i]["high"] - bars[i]["low"],
            abs(bars[i]["high"] - prev),
            abs(bars[i]["low"] - prev),
        )
    return out


def atr(bars, period):
    return wilder(true_range(bars), period)


def keltner(bars, period, mult, atr_period):
    basis = ema(source(bars, "close"), period)
    span = atr(bars, atr_period)

    upper, middle, lower = [], [], []
    for mean, band in zip(basis, span, strict=True):
        if mean is None or band is None:
            upper.append(None)
            middle.append(None)
            lower.append(None)
        else:
            upper.append(mean + mult * band)
            middle.append(mean)
            lower.append(mean - mult * band)

    return upper, middle, lower


def donchian(bars, period):
    n = len(bars)
    upper, middle, lower = [None] * n, [None] * n, [None] * n
    for i in range(period - 1, n):
        window = bars[i - period + 1 : i + 1]
        top = max(b["high"] for b in window)
        bottom = min(b["low"] for b in window)
        upper[i], middle[i], lower[i] = top, (top + bottom) / 2, bottom
    return upper, middle, lower


def rolling_vwap(bars, period):
    out = [None] * len(bars)
    prices = typical(bars)
    for i in range(period - 1, len(bars)):
        window = range(i - period + 1, i + 1)
        volume = sum(bars[j]["volume"] for j in window)
        if volume <= 0:
            out[i] = sum(prices[j] for j in window) / period
        else:
            out[i] = sum(prices[j] * bars[j]["volume"] for j in window) / volume
    return out


def vwma(bars, values, period):
    out = [None] * len(bars)
    for i in range(period - 1, len(bars)):
        window = range(i - period + 1, i + 1)
        volume = sum(bars[j]["volume"] for j in window)
        if volume <= 0:
            out[i] = sum(values[j] for j in window) / period
        else:
            out[i] = sum(values[j] * bars[j]["volume"] for j in window) / volume
    return out


def volume_ma(bars, period):
    return sma([b["volume"] for b in bars], period)


def psar(bars, step, ceiling):
    n = len(bars)
    line, heading = [None] * n, [None] * n
    if n < 2:
        return line, heading

    rising = bars[1]["close"] >= bars[0]["close"]
    if rising:
        sar = min(bars[0]["low"], bars[1]["low"])
        extreme = max(bars[0]["high"], bars[1]["high"])
    else:
        sar = max(bars[0]["high"], bars[1]["high"])
        extreme = min(bars[0]["low"], bars[1]["low"])
    rate = step
    line[1], heading[1] = sar, 1.0 if rising else -1.0

    for i in range(2, n):
        current, previous, older = bars[i], bars[i - 1], bars[i - 2]
        sar += rate * (extreme - sar)

        if rising:
            sar = min(sar, previous["low"], older["low"])
            if current["low"] < sar:
                rising = False
                sar, extreme, rate = extreme, current["low"], step
            elif current["high"] > extreme:
                extreme = current["high"]
                rate = min(rate + step, ceiling)
        else:
            sar = max(sar, previous["high"], older["high"])
            if current["high"] > sar:
                rising = True
                sar, extreme, rate = extreme, current["high"], step
            elif current["low"] < extreme:
                extreme = current["low"]
                rate = min(rate + step, ceiling)

        line[i], heading[i] = sar, 1.0 if rising else -1.0

    return line, heading


def supertrend(bars, period, mult):
    span = atr(bars, period)
    n = len(bars)
    line, heading = [None] * n, [None] * n

    started, rising = False, False
    final_upper = final_lower = last_close = 0.0

    for i, bar in enumerate(bars):
        if span[i] is None:
            continue

        middle = (bar["high"] + bar["low"]) / 2
        band = mult * span[i]
        upper, lower = middle + band, middle - band

        if not started:
            final_upper, final_lower = upper, lower
            rising = bar["close"] > upper
            started = True
        else:
            if upper < final_upper or last_close > final_upper:
                final_upper = upper
            if lower > final_lower or last_close < final_lower:
                final_lower = lower
            if rising and bar["close"] < final_lower:
                rising = False
            elif not rising and bar["close"] > final_upper:
                rising = True

        last_close = bar["close"]
        line[i] = final_lower if rising else final_upper
        heading[i] = 1.0 if rising else -1.0

    return line, heading


def ichimoku(bars, tenkan_period, kijun_period, senkou_period, displacement):
    n = len(bars)

    def midpoint(period, i):
        if i + 1 < period:
            return None
        window = bars[i - period + 1 : i + 1]
        return (max(b["high"] for b in window) + min(b["low"] for b in window)) / 2

    tenkan = [midpoint(tenkan_period, i) for i in range(n)]
    kijun = [midpoint(kijun_period, i) for i in range(n)]
    raw_a = [
        None if tenkan[i] is None or kijun[i] is None else (tenkan[i] + kijun[i]) / 2
        for i in range(n)
    ]
    raw_b = [midpoint(senkou_period, i) for i in range(n)]

    senkou_a = [None] * n
    senkou_b = [None] * n
    for i in range(displacement, n):
        senkou_a[i] = raw_a[i - displacement]
        senkou_b[i] = raw_b[i - displacement]

    for i in range(n):
        if senkou_a[i] is None or senkou_b[i] is None:
            tenkan[i] = kijun[i] = senkou_a[i] = senkou_b[i] = None

    return tenkan, kijun, senkou_a, senkou_b


def stochastic(bars, k_period, smooth, d_period):
    n = len(bars)
    raw = [None] * n
    for i in range(k_period - 1, n):
        window = bars[i - k_period + 1 : i + 1]
        top = max(b["high"] for b in window)
        bottom = min(b["low"] for b in window)
        if top <= bottom:
            raw[i] = 50.0
        else:
            raw[i] = 100 * (bars[i]["close"] - bottom) / (top - bottom)

    start = k_period - 1
    k_line = [None] * start + sma(raw[start:], smooth)
    start += smooth - 1
    d_line = [None] * start + sma(k_line[start:], d_period)

    return k_line, d_line


def stoch_rsi(values, rsi_period, stoch_period, k_period, d_period):
    n = len(values)
    strength = rsi(values, rsi_period)
    first = first_value(strength)

    raw = [None] * n
    for i in range(first + stoch_period - 1, n):
        window = strength[i - stoch_period + 1 : i + 1]
        top, bottom = max(window), min(window)
        if top <= bottom:
            raw[i] = 50.0
        else:
            raw[i] = 100 * (strength[i] - bottom) / (top - bottom)

    start = first + stoch_period - 1
    k_line = [None] * start + sma(raw[start:], k_period)
    start += k_period - 1
    d_line = [None] * start + sma(k_line[start:], d_period)

    return k_line, d_line


def cci(bars, period):
    prices = typical(bars)
    out = [None] * len(bars)
    for i in range(period - 1, len(bars)):
        window = prices[i - period + 1 : i + 1]
        mean = sum(window) / period
        spread = sum(abs(x - mean) for x in window) / period
        out[i] = 0.0 if spread == 0 else (prices[i] - mean) / (0.015 * spread)
    return out


def williams_r(bars, period):
    out = [None] * len(bars)
    for i in range(period - 1, len(bars)):
        window = bars[i - period + 1 : i + 1]
        top = max(b["high"] for b in window)
        bottom = min(b["low"] for b in window)
        if top <= bottom:
            out[i] = -50.0
        else:
            out[i] = -100 * (top - bars[i]["close"]) / (top - bottom)
    return out


def roc(values, period):
    out = [None] * len(values)
    for i in range(period, len(values)):
        past = values[i - period]
        out[i] = 0.0 if past == 0 else 100 * (values[i] - past) / past
    return out


def momentum(values, period):
    out = [None] * len(values)
    for i in range(period, len(values)):
        out[i] = values[i] - values[i - period]
    return out


def adx(bars, period):
    n = len(bars)
    span, plus_dm, minus_dm = [None] * n, [None] * n, [None] * n

    for i in range(1, n):
        current, previous = bars[i], bars[i - 1]
        up = current["high"] - previous["high"]
        down = previous["low"] - current["low"]
        plus_dm[i] = up if up > down and up > 0 else 0.0
        minus_dm[i] = down if down > up and down > 0 else 0.0
        span[i] = max(
            current["high"] - current["low"],
            abs(current["high"] - previous["close"]),
            abs(current["low"] - previous["close"]),
        )

    smoothed = wilder(span, period)
    smoothed_plus = wilder(plus_dm, period)
    smoothed_minus = wilder(minus_dm, period)

    plus_di, minus_di, index = [None] * n, [None] * n, [None] * n
    for i in range(n):
        if smoothed[i] is None:
            continue
        if smoothed[i] > 0:
            plus_di[i] = 100 * smoothed_plus[i] / smoothed[i]
            minus_di[i] = 100 * smoothed_minus[i] / smoothed[i]
        else:
            plus_di[i] = minus_di[i] = 0.0
        total = plus_di[i] + minus_di[i]
        index[i] = 0.0 if total <= 0 else 100 * abs(plus_di[i] - minus_di[i]) / total

    return wilder(index, period), plus_di, minus_di


def aroon(bars, period):
    n = len(bars)
    up, down, oscillator = [None] * n, [None] * n, [None] * n

    for i in range(period, n):
        highs = [b["high"] for b in bars[i - period : i + 1]]
        lows = [b["low"] for b in bars[i - period : i + 1]]
        newest_high = max(j for j, v in enumerate(highs) if v == max(highs))
        newest_low = max(j for j, v in enumerate(lows) if v == min(lows))
        up[i] = 100 * newest_high / period
        down[i] = 100 * newest_low / period
        oscillator[i] = up[i] - down[i]

    return up, down, oscillator


def chaikin_volatility(bars, period, change):
    spread = ema([b["high"] - b["low"] for b in bars], period)
    out = [None] * len(bars)
    for i in range(period - 1 + change, len(bars)):
        past = spread[i - change]
        out[i] = 0.0 if past == 0 else 100 * (spread[i] - past) / past
    return out


def obv(bars):
    out = [None] * len(bars)
    total = 0.0
    for i in range(1, len(bars)):
        if bars[i]["close"] > bars[i - 1]["close"]:
            total += bars[i]["volume"]
        elif bars[i]["close"] < bars[i - 1]["close"]:
            total -= bars[i]["volume"]
        out[i] = total
    return out


def ad_line(bars):
    out = []
    total = 0.0
    for bar in bars:
        if bar["high"] > bar["low"]:
            total += (
                ((bar["close"] - bar["low"]) - (bar["high"] - bar["close"]))
                / (bar["high"] - bar["low"])
                * bar["volume"]
            )
        out.append(total)
    return out


def chaikin_oscillator(bars, fast, slow):
    line = ad_line(bars)
    quick = ema(line, fast)
    steady = ema(line, slow)
    return [
        None if a is None or b is None else a - b
        for a, b in zip(quick, steady, strict=True)
    ]


def mfi(bars, period):
    n = len(bars)
    prices = typical(bars)
    positive, negative = [None] * n, [None] * n

    for i in range(1, n):
        flow = prices[i] * bars[i]["volume"]
        positive[i] = flow if prices[i] > prices[i - 1] else 0.0
        negative[i] = flow if prices[i] < prices[i - 1] else 0.0

    out = [None] * n
    for i in range(period, n):
        up = sum(positive[i - period + 1 : i + 1])
        down = sum(negative[i - period + 1 : i + 1])
        out[i] = 100.0 if down == 0 else 100 - 100 / (1 + up / down)
    return out


PIVOT_OUTPUTS = ["pivot", "r1", "r2", "r3", "s1", "s2", "s3"]


def pivot_points(bars, period, fibonacci):
    n = len(bars)
    out = {name: [None] * n for name in PIVOT_OUTPUTS}

    for i in range(period, n):
        window = bars[i - period : i]
        high = max(b["high"] for b in window)
        low = min(b["low"] for b in window)
        pivot = (high + low + window[-1]["close"]) / 3
        span = high - low

        if fibonacci:
            values = [
                pivot,
                pivot + 0.382 * span,
                pivot + 0.618 * span,
                pivot + 1.0 * span,
                pivot - 0.382 * span,
                pivot - 0.618 * span,
                pivot - 1.0 * span,
            ]
        else:
            values = [
                pivot,
                2 * pivot - low,
                pivot + span,
                high + 2 * (pivot - low),
                2 * pivot - high,
                pivot - span,
                low - 2 * (high - pivot),
            ]

        for name, value in zip(PIVOT_OUTPUTS, values, strict=True):
            out[name][i] = value

    return out


def fractals(bars, period):
    n = len(bars)
    span = 2 * period + 1
    up, down = [None] * n, [None] * n
    level_up = level_down = None

    for i in range(span - 1, n):
        window = bars[i - span + 1 : i + 1]
        highs = [b["high"] for b in window]
        lows = [b["low"] for b in window]

        if level_up is None:
            level_up, level_down = max(highs), min(lows)
        if all(highs[j] < highs[period] for j in range(span) if j != period):
            level_up = highs[period]
        if all(lows[j] > lows[period] for j in range(span) if j != period):
            level_down = lows[period]

        up[i], down[i] = level_up, level_down

    return up, down


def zigzag(bars, deviation):
    threshold = deviation / 100
    n = len(bars)
    line, heading = [None] * n, [None] * n

    def moved(anchor, price):
        return anchor > 0 and abs(price - anchor) / anchor >= threshold

    pivot = extreme = bars[0]["close"]
    direction = 0.0
    line[0], heading[0] = extreme, direction

    for i in range(1, n):
        bar = bars[i]
        if direction > 0:
            extreme = max(extreme, bar["high"])
            if moved(extreme, bar["low"]):
                pivot, extreme, direction = extreme, bar["low"], -1.0
        elif direction < 0:
            extreme = min(extreme, bar["low"])
            if moved(extreme, bar["high"]):
                pivot, extreme, direction = extreme, bar["high"], 1.0
        elif bar["high"] > pivot and moved(pivot, bar["high"]):
            extreme, direction = bar["high"], 1.0
        elif bar["low"] < pivot and moved(pivot, bar["low"]):
            extreme, direction = bar["low"], -1.0

        line[i], heading[i] = extreme, direction

    return line, heading


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
    kc_upper, kc_middle, kc_lower = keltner(bars, 20, 2.0, 10)
    dc_upper, dc_middle, dc_lower = donchian(bars, 20)
    sar_line, sar_direction = psar(bars, 0.02, 0.2)
    st_line, st_direction = supertrend(bars, 10, 3.0)
    tenkan, kijun, senkou_a, senkou_b = ichimoku(bars, 9, 26, 52, 26)
    stoch_k, stoch_d = stochastic(bars, 14, 3, 3)
    quick_k, quick_d = stochastic(bars, 5, 1, 3)
    srsi_k, srsi_d = stoch_rsi(close, 14, 14, 3, 3)
    adx_line, plus_di, minus_di = adx(bars, 14)
    aroon_up, aroon_down, aroon_osc = aroon(bars, 25)
    fractal_up, fractal_down = fractals(bars, 2)
    zigzag_line, zigzag_direction = zigzag(bars, 5.0)
    classic = pivot_points(bars, 1, False)
    fibonacci = pivot_points(bars, 1, True)
    weekly = pivot_points(bars, 5, False)

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
        case("wma:20", {"wma": wma(close, 20)}),
        case("wma:10:source=hl2", {"wma": wma(hl2, 10)}),
        case("hma:9", {"hma": hma(close, 9)}),
        case("dema:20", {"dema": dema(close, 20)}),
        case("tema:20", {"tema": tema(close, 20)}),
        case("vwap:20", {"vwap": rolling_vwap(bars, 20)}),
        case(
            "keltner:20:2:10",
            {"upper": kc_upper, "middle": kc_middle, "lower": kc_lower},
        ),
        case(
            "donchian:20",
            {"upper": dc_upper, "middle": dc_middle, "lower": dc_lower},
        ),
        case("psar:0.02:0.2", {"sar": sar_line, "direction": sar_direction}),
        case(
            "supertrend:10:3",
            {"supertrend": st_line, "direction": st_direction},
        ),
        case(
            "ichimoku:9:26:52:26",
            {
                "tenkan": tenkan,
                "kijun": kijun,
                "senkou_a": senkou_a,
                "senkou_b": senkou_b,
            },
        ),
        case("stoch:14:3:3", {"k": stoch_k, "d": stoch_d}),
        case("stoch:5:1:3", {"k": quick_k, "d": quick_d}),
        case("stochrsi:14:14:3:3", {"k": srsi_k, "d": srsi_d}),
        case("cci:20", {"cci": cci(bars, 20)}),
        case("willr:14", {"willr": williams_r(bars, 14)}),
        case("roc:12", {"roc": roc(close, 12)}),
        case("mom:10", {"mom": momentum(close, 10)}),
        case(
            "adx:14",
            {"adx": adx_line, "plus_di": plus_di, "minus_di": minus_di},
        ),
        case(
            "aroon:25",
            {"up": aroon_up, "down": aroon_down, "oscillator": aroon_osc},
        ),
        case("atr:14", {"atr": atr(bars, 14)}),
        case("stddev:20", {"stddev": stddev(close, 20)}),
        case("hv:20:252", {"hv": historical_volatility(close, 20, 252)}),
        case("cvol:10:10", {"cvol": chaikin_volatility(bars, 10, 10)}),
        case("obv", {"obv": obv(bars)}),
        case("mfi:14", {"mfi": mfi(bars, 14)}),
        case("ad", {"ad": ad_line(bars)}),
        case("chaikin:3:10", {"chaikin": chaikin_oscillator(bars, 3, 10)}),
        case("volma:20", {"volma": volume_ma(bars, 20)}),
        case("vwma:20", {"vwma": vwma(bars, close, 20)}),
        case("pivots:1", classic),
        case("fibpivots:1", fibonacci),
        case("pivots:5", weekly),
        case("fractals:2", {"up": fractal_up, "down": fractal_down}),
        case("zigzag:5", {"zigzag": zigzag_line, "direction": zigzag_direction}),
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
