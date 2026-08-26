package strategy

import (
	"encoding/json"
	"strings"

	"github.com/mhetem/ALVO-Backtester/internal/indicator"
)

const (
	Version    = 1
	MaxInputs  = 12
	MaxNodes   = 200
	MaxDepth   = 8
	MaxNameLen = 24
	MaxBack    = 500
)

type Field string

const FieldVolume Field = "volume"

var Fields = buildFields()

func buildFields() []Field {
	fields := make([]Field, 0, len(indicator.Sources)+1)
	for _, source := range indicator.Sources {
		fields = append(fields, Field(source))
	}
	return append(fields, FieldVolume)
}

func (f Field) Valid() bool {
	return f == FieldVolume || indicator.Source(f).Valid()
}

func (f Field) Value(c indicator.Candle) float64 {
	if f == FieldVolume {
		return c.Volume
	}
	return indicator.Source(f).Value(c)
}

func JoinFields() string {
	names := make([]string, 0, len(Fields))
	for _, field := range Fields {
		names = append(names, string(field))
	}
	return strings.Join(names, ", ")
}

type Comparator string

const (
	OpGT           Comparator = "gt"
	OpLT           Comparator = "lt"
	OpGTE          Comparator = "gte"
	OpLTE          Comparator = "lte"
	OpEQ           Comparator = "eq"
	OpCrossesAbove Comparator = "crosses_above"
	OpCrossesBelow Comparator = "crosses_below"
	OpRising       Comparator = "rising"
	OpFalling      Comparator = "falling"
	OpBetween      Comparator = "between"
)

var Comparators = []Comparator{
	OpGT, OpLT, OpGTE, OpLTE, OpEQ,
	OpCrossesAbove, OpCrossesBelow,
	OpRising, OpFalling, OpBetween,
}

const (
	KeyAll        = "all"
	KeyAny        = "any"
	KeyNot        = "not"
	KeyRef        = "ref"
	KeyStopLoss   = "stop_loss"
	KeyTakeProfit = "take_profit"
	KeyLong       = "long"
	KeyShort      = "short"

	LineSep = "."
)

var Combinators = []string{KeyAll, KeyAny, KeyNot}

type SizingType string

const (
	SizeFixedQty  SizingType = "fixed_qty"
	SizePctEquity SizingType = "pct_equity"
	SizeFixedCash SizingType = "fixed_cash"
	SizeRiskPct   SizingType = "risk_pct"
)

var SizingTypes = []SizingType{SizeFixedQty, SizePctEquity, SizeFixedCash, SizeRiskPct}

type LevelType string

const (
	LevelPct LevelType = "pct"
	LevelATR LevelType = "atr"
)

var LevelTypes = []LevelType{LevelPct, LevelATR}

const (
	DefaultFeeBPS      = 3.25
	DefaultSlippageBPS = 5.0
	MaxBPS             = 1000.0
	MaxBrokerageCents  = 1_000_000
	ATRIndicator       = "atr"
)

type Spec struct {
	Version int              `json:"version"`
	Inputs  map[string]Input `json:"inputs"`
	Entry   Side             `json:"entry"`
	Exit    *Side            `json:"exit,omitempty"`
	Sizing  Sizing           `json:"sizing"`
	Costs   Costs            `json:"costs"`
}

type Side struct {
	Long  Node
	Short Node
}

func (s Side) node(key string) Node {
	if key == KeyShort {
		return s.Short
	}
	return s.Long
}

func (s Side) MarshalJSON() ([]byte, error) {
	body := map[string]Node{}
	if s.Long != nil {
		body[KeyLong] = s.Long
	}
	if s.Short != nil {
		body[KeyShort] = s.Short
	}

	return json.Marshal(body)
}

type Input struct {
	Indicator string
	Params    map[string]float64
	Source    string
	Output    string
	Sourced   bool
	Multi     bool
}

func (i Input) MarshalJSON() ([]byte, error) {
	body := struct {
		Indicator string             `json:"indicator"`
		Params    map[string]float64 `json:"params"`
		Source    string             `json:"source,omitempty"`
		Output    string             `json:"output,omitempty"`
	}{Indicator: i.Indicator, Params: i.Params}

	if body.Params == nil {
		body.Params = map[string]float64{}
	}
	if i.Sourced {
		body.Source = i.Source
	}
	if i.Multi {
		body.Output = i.Output
	}

	return json.Marshal(body)
}

type Sizing struct {
	Type  SizingType `json:"type"`
	Value float64    `json:"value"`
}

type Costs struct {
	BrokerageCents int64   `json:"brokerage_cents"`
	FeeBPS         float64 `json:"fee_bps"`
	SlippageBPS    float64 `json:"slippage_bps"`
}

func DefaultCosts() Costs {
	return Costs{BrokerageCents: 0, FeeBPS: DefaultFeeBPS, SlippageBPS: DefaultSlippageBPS}
}

type OperandKind int

const (
	OperandInput OperandKind = iota
	OperandField
	OperandLiteral
)

type Operand struct {
	Kind   OperandKind
	Input  string
	Output string
	Field  Field
	Value  float64
	Back   int
}

// Ref is how an operand names itself: an input on its own reads that input's declared
// output, and "input.line" reads any other line the same indicator emits.
func (o Operand) Ref() string {
	if o.Output == "" {
		return o.Input
	}
	return o.Input + LineSep + o.Output
}

func (o Operand) MarshalJSON() ([]byte, error) {
	var head any
	switch o.Kind {
	case OperandInput:
		head = o.Ref()
	case OperandField:
		head = string(o.Field)
	default:
		head = o.Value
	}

	if o.Back == 0 {
		return json.Marshal(head)
	}
	return json.Marshal(map[string][]any{KeyRef: {head, o.Back}})
}

type Node interface {
	node()
}

type All struct{ Nodes []Node }

type Any struct{ Nodes []Node }

type Not struct{ Node Node }

type Compare struct {
	Op       Comparator
	Operands []Operand
}

type StopLoss struct{ Level Level }

type TakeProfit struct{ Level Level }

type Level struct {
	Type   LevelType
	Value  float64
	Period int
	Mult   float64
}

func (All) node()        {}
func (Any) node()        {}
func (Not) node()        {}
func (Compare) node()    {}
func (StopLoss) node()   {}
func (TakeProfit) node() {}

func (a All) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string][]Node{KeyAll: a.Nodes})
}

func (a Any) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string][]Node{KeyAny: a.Nodes})
}

func (n Not) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]Node{KeyNot: n.Node})
}

func (c Compare) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string][]Operand{string(c.Op): c.Operands})
}

func (s StopLoss) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]Level{KeyStopLoss: s.Level})
}

func (t TakeProfit) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]Level{KeyTakeProfit: t.Level})
}

func (l Level) MarshalJSON() ([]byte, error) {
	if l.Type == LevelATR {
		return json.Marshal(struct {
			Type   LevelType `json:"type"`
			Period int       `json:"period"`
			Mult   float64   `json:"mult"`
		}{Type: l.Type, Period: l.Period, Mult: l.Mult})
	}

	return json.Marshal(struct {
		Type  LevelType `json:"type"`
		Value float64   `json:"value"`
	}{Type: l.Type, Value: l.Value})
}

func arity(op Comparator) (int, int) {
	switch op {
	case OpRising, OpFalling:
		return 1, 2
	case OpBetween:
		return 3, 3
	default:
		return 2, 2
	}
}

func known(op Comparator) bool {
	for _, candidate := range Comparators {
		if candidate == op {
			return true
		}
	}
	return false
}

func joinComparators() string {
	names := make([]string, 0, len(Comparators))
	for _, op := range Comparators {
		names = append(names, string(op))
	}
	return strings.Join(names, ", ")
}

func joinSizingTypes() string {
	names := make([]string, 0, len(SizingTypes))
	for _, kind := range SizingTypes {
		names = append(names, string(kind))
	}
	return strings.Join(names, ", ")
}

func joinLevelTypes() string {
	names := make([]string, 0, len(LevelTypes))
	for _, kind := range LevelTypes {
		names = append(names, string(kind))
	}
	return strings.Join(names, ", ")
}
