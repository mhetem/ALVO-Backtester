package market

import "testing"

func TestClassifyTicker(t *testing.T) {
	cases := []struct {
		ticker string
		want   Kind
	}{
		{"PETR4", KindStock},
		{"MGLU3", KindStock},
		{"BBDC4", KindStock},
		{"petr4", KindStock},
		{"BPAC11", KindUnit},
		{"SANB11", KindUnit},
		{"KLBN11", KindUnit},
		{"TAEE11", KindUnit},
		{"AAPL34", KindBDR},
		{"MSFT34", KindBDR},
		{"ROXO31", KindBDR},
		{"^BVSP", KindIndex},
		{"WINZ25", KindFuture},
		{"WDOV26", KindFuture},
		{"WIN", KindFuture},
		{"DOL", KindFuture},
		{"BTC-BRL", KindCrypto},
	}

	for _, tc := range cases {
		got, err := ClassifyTicker(tc.ticker)
		if err != nil {
			t.Errorf("ClassifyTicker(%q): %v", tc.ticker, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ClassifyTicker(%q) = %s, want %s", tc.ticker, got, tc.want)
		}
	}
}

func TestClassifyTickerRejectsUnknownShapes(t *testing.T) {
	for _, ticker := range []string{"", "   ", "PETR", "PETR4F", "PETR44", "TOOLONG11", "12345"} {
		if kind, err := ClassifyTicker(ticker); err == nil {
			t.Errorf("ClassifyTicker(%q) = %s, want an error", ticker, kind)
		}
	}
}

func TestDefaultLotSize(t *testing.T) {
	cases := map[Kind]int32{
		KindStock:  100,
		KindUnit:   100,
		KindFII:    1,
		KindBDR:    1,
		KindIndex:  1,
		KindFuture: 1,
		KindCrypto: 1,
	}

	for kind, want := range cases {
		if got := DefaultLotSize(kind); got != want {
			t.Errorf("DefaultLotSize(%s) = %d, want %d", kind, got, want)
		}
	}
}

func TestParseKind(t *testing.T) {
	if kind, err := ParseKind(" STOCK "); err != nil || kind != KindStock {
		t.Errorf("ParseKind(\" STOCK \") = %s, %v", kind, err)
	}
	if _, err := ParseKind("etf"); err == nil {
		t.Error("ParseKind(\"etf\") succeeded, want an error")
	}
}
