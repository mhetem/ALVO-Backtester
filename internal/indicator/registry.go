package indicator

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type ParamKind string

const (
	ParamInt   ParamKind = "int"
	ParamFloat ParamKind = "float"
)

type Group string

const (
	GroupOverlay    Group = "overlay"
	GroupMomentum   Group = "momentum"
	GroupVolatility Group = "volatility"
	GroupVolume     Group = "volume"
	GroupStructure  Group = "structure"
)

var Groups = []Group{GroupOverlay, GroupMomentum, GroupVolatility, GroupVolume, GroupStructure}

func (g Group) String() string { return string(g) }

type Param struct {
	Name    string
	Kind    ParamKind
	Default float64
	Min     float64
	Max     float64
}

type Spec struct {
	Name     string
	Title    string
	Group    Group
	Overlay  bool
	Sourced  bool
	Params   []Param
	Outputs  []string
	Offsets  func(Params) []int
	Validate func(Params) error
	New      func(Params) Indicator
}

func (s Spec) offsets(params Params) []int {
	out := make([]int, len(s.Outputs))
	if s.Offsets == nil {
		return out
	}
	copy(out, s.Offsets(params))
	return out
}

type Params struct {
	values map[string]float64
	source Source
}

func (p Params) Int(name string) int { return int(p.values[name]) }

func (p Params) Float(name string) float64 { return p.values[name] }

func (p Params) Source() Source {
	if p.source == "" {
		return DefaultSource
	}
	return p.source
}

func (p Params) All() map[string]float64 {
	out := make(map[string]float64, len(p.values))
	for name, value := range p.values {
		out[name] = value
	}
	return out
}

var registry = struct {
	sync.RWMutex
	specs map[string]Spec
}{specs: map[string]Spec{}}

func Register(spec Spec) {
	if err := spec.validate(); err != nil {
		panic("indicator: " + err.Error())
	}

	registry.Lock()
	defer registry.Unlock()

	if _, taken := registry.specs[spec.Name]; taken {
		panic("indicator: " + spec.Name + " is registered twice")
	}
	registry.specs[spec.Name] = spec
}

func Lookup(name string) (Spec, bool) {
	registry.RLock()
	defer registry.RUnlock()

	spec, ok := registry.specs[strings.ToLower(strings.TrimSpace(name))]
	return spec, ok
}

func Catalog() []Spec {
	registry.RLock()
	defer registry.RUnlock()

	specs := make([]Spec, 0, len(registry.specs))
	for _, spec := range registry.specs {
		specs = append(specs, spec)
	}
	slices.SortFunc(specs, func(a, b Spec) int { return strings.Compare(a.Name, b.Name) })

	return specs
}

func Names() []string {
	specs := Catalog()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

func (s Spec) validate() error {
	switch {
	case s.Name != strings.ToLower(strings.TrimSpace(s.Name)) || s.Name == "":
		return fmt.Errorf("%q is not a usable indicator name", s.Name)
	case len(s.Outputs) == 0:
		return fmt.Errorf("%s declares no outputs", s.Name)
	case s.New == nil:
		return fmt.Errorf("%s has no constructor", s.Name)
	}

	for _, param := range s.Params {
		switch {
		case param.Name == "" || param.Name == sourceParam:
			return fmt.Errorf("%s has an unusable parameter name %q", s.Name, param.Name)
		case param.Min > param.Max:
			return fmt.Errorf("%s parameter %s has min %v above max %v", s.Name, param.Name, param.Min, param.Max)
		case param.Default < param.Min || param.Default > param.Max:
			return fmt.Errorf("%s parameter %s defaults to %v, outside %v..%v", s.Name, param.Name, param.Default, param.Min, param.Max)
		}
	}

	return nil
}

func (s Spec) param(name string) (Param, bool) {
	for _, param := range s.Params {
		if param.Name == name {
			return param, true
		}
	}
	return Param{}, false
}

func (s Spec) resolve(values map[string]float64, source Source) (Params, error) {
	resolved := Params{values: make(map[string]float64, len(s.Params)), source: DefaultSource}

	for _, param := range s.Params {
		value, ok := values[param.Name]
		if !ok {
			value = param.Default
		}
		if value < param.Min || value > param.Max {
			return Params{}, fmt.Errorf("%s: %s must be between %s and %s, got %s",
				s.Name, param.Name, param.format(param.Min), param.format(param.Max), param.format(value))
		}
		if param.Kind == ParamInt && value != float64(int(value)) {
			return Params{}, fmt.Errorf("%s: %s must be a whole number, got %s", s.Name, param.Name, param.format(value))
		}
		resolved.values[param.Name] = value
	}

	for name := range values {
		if _, known := s.param(name); !known {
			return Params{}, fmt.Errorf("%s has no parameter %q (want one of: %s)", s.Name, name, s.paramNames())
		}
	}

	if source != "" {
		if !s.Sourced {
			return Params{}, fmt.Errorf("%s does not take a source", s.Name)
		}
		if !source.Valid() {
			return Params{}, fmt.Errorf("unknown source %q (want one of: %s)", source, JoinSources())
		}
		resolved.source = source
	}

	if s.Validate != nil {
		if err := s.Validate(resolved); err != nil {
			return Params{}, fmt.Errorf("%s: %w", s.Name, err)
		}
	}

	return resolved, nil
}

func (s Spec) paramNames() string {
	names := make([]string, 0, len(s.Params))
	for _, param := range s.Params {
		names = append(names, param.Name)
	}
	if s.Sourced {
		names = append(names, sourceParam)
	}
	return strings.Join(names, ", ")
}

func (p Param) format(value float64) string {
	if p.Kind == ParamInt {
		return strconv.Itoa(int(value))
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}
