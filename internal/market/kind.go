package market

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type Kind string

const (
	KindStock  Kind = "stock"
	KindFII    Kind = "fii"
	KindBDR    Kind = "bdr"
	KindUnit   Kind = "unit"
	KindIndex  Kind = "index"
	KindFuture Kind = "future"
	KindCrypto Kind = "crypto"
)

const DefaultCurrency = "BRL"

var Kinds = []Kind{KindStock, KindFII, KindBDR, KindUnit, KindIndex, KindFuture, KindCrypto}

var DefaultFutureRoots = []string{"WIN", "IND", "WDO", "DOL"}

func (k Kind) Valid() bool { return slices.Contains(Kinds, k) }

func (k Kind) String() string { return string(k) }

func ParseKind(text string) (Kind, error) {
	kind := Kind(strings.ToLower(strings.TrimSpace(text)))
	if !kind.Valid() {
		names := make([]string, 0, len(Kinds))
		for _, k := range Kinds {
			names = append(names, string(k))
		}
		return "", fmt.Errorf("unknown symbol kind %q (want one of: %s)", text, strings.Join(names, ", "))
	}
	return kind, nil
}

func DefaultLotSize(kind Kind) int32 {
	switch kind {
	case KindStock, KindUnit:
		return 100
	default:
		return 1
	}
}

func DefaultTickSize(kind Kind) float64 {
	switch kind {
	case KindIndex:
		return 1
	default:
		return 0.01
	}
}

var (
	indexPattern  = regexp.MustCompile(`^\^[A-Z0-9]{3,6}$`)
	cryptoPattern = regexp.MustCompile(`^[A-Z0-9]{2,10}-[A-Z]{3}$`)
	bdrPattern    = regexp.MustCompile(`^[A-Z][A-Z0-9]{3}(31|32|33|34|35|39)$`)
	unitPattern   = regexp.MustCompile(`^[A-Z][A-Z0-9]{3}11$`)
	stockPattern  = regexp.MustCompile(`^[A-Z][A-Z0-9]{3}[345678]$`)
	futurePattern = regexp.MustCompile(`^([A-Z]{3})[FGHJKMNQUVXZ]\d{2}$`)
)

func ClassifyTicker(ticker string) (Kind, error) {
	normalised := strings.ToUpper(strings.TrimSpace(ticker))
	if normalised == "" {
		return "", errors.New("empty ticker")
	}

	switch {
	case indexPattern.MatchString(normalised):
		return KindIndex, nil
	case cryptoPattern.MatchString(normalised):
		return KindCrypto, nil
	case bdrPattern.MatchString(normalised):
		return KindBDR, nil
	case unitPattern.MatchString(normalised):
		return KindUnit, nil
	case stockPattern.MatchString(normalised):
		return KindStock, nil
	}

	if match := futurePattern.FindStringSubmatch(normalised); match != nil && slices.Contains(DefaultFutureRoots, match[1]) {
		return KindFuture, nil
	}
	if slices.Contains(DefaultFutureRoots, normalised) {
		return KindFuture, nil
	}

	return "", fmt.Errorf("cannot classify ticker %q", normalised)
}
