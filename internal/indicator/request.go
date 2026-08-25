package indicator

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	sourceParam   = "source"
	MaxInstances  = 8
	MaxPrimeBars  = 5000
	pathPrimeBars = 250
)

type Primer interface {
	PrimeBars() int
}

type Anchorer interface {
	Anchor()
}

func Anchor(ind Indicator) {
	if anchorer, ok := ind.(Anchorer); ok {
		anchorer.Anchor()
	}
}

type Instance struct {
	Key       string
	Spec      Spec
	Params    Params
	Offsets   []int
	Indicator Indicator
}

func New(name string, values map[string]float64, source Source) (Instance, error) {
	spec, ok := Lookup(name)
	if !ok {
		return Instance{}, fmt.Errorf("unknown indicator %q (want one of: %s)", name, strings.Join(Names(), ", "))
	}

	params, err := spec.resolve(values, source)
	if err != nil {
		return Instance{}, err
	}

	return Instance{
		Key:       spec.key(params),
		Spec:      spec,
		Params:    params,
		Offsets:   spec.offsets(params),
		Indicator: spec.New(params),
	}, nil
}

func Parse(text string) (Instance, error) {
	parts := strings.Split(strings.TrimSpace(text), ":")
	name := strings.ToLower(strings.TrimSpace(parts[0]))
	if name == "" {
		return Instance{}, fmt.Errorf("%q names no indicator (want something like ema:9)", text)
	}

	spec, ok := Lookup(name)
	if !ok {
		return Instance{}, fmt.Errorf("unknown indicator %q (want one of: %s)", name, strings.Join(Names(), ", "))
	}

	values := map[string]float64{}
	source := Source("")
	positional := 0

	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		if part == "" {
			return Instance{}, fmt.Errorf("%s: empty parameter in %q", name, text)
		}

		key, raw, named := strings.Cut(part, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		raw = strings.TrimSpace(raw)

		if !named {
			if positional >= len(spec.Params) {
				return Instance{}, fmt.Errorf("%s takes %s, so %q has too many", name, spec.arity(), text)
			}
			key, raw = spec.Params[positional].Name, part
			positional++
		}

		if key == sourceParam {
			parsed, err := ParseSource(raw)
			if err != nil {
				return Instance{}, fmt.Errorf("%s: %w", name, err)
			}
			source = parsed
			continue
		}

		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return Instance{}, fmt.Errorf("%s: %s must be a number, got %q", name, key, raw)
		}
		if _, taken := values[key]; taken {
			return Instance{}, fmt.Errorf("%s: %s is set twice in %q", name, key, text)
		}
		values[key] = value
	}

	return New(name, values, source)
}

func ParseList(text string) ([]Instance, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}

	instances := []Instance{}
	seen := map[string]bool{}

	for _, entry := range strings.Split(text, ",") {
		if strings.TrimSpace(entry) == "" {
			continue
		}

		instance, err := Parse(entry)
		if err != nil {
			return nil, err
		}
		if seen[instance.Key] {
			continue
		}

		seen[instance.Key] = true
		instances = append(instances, instance)

		if len(instances) > MaxInstances {
			return nil, fmt.Errorf("at most %d indicators per request", MaxInstances)
		}
	}

	return instances, nil
}

func MaxOffset(instances []Instance) int {
	ahead := 0
	for _, instance := range instances {
		for _, offset := range instance.Offsets {
			ahead = max(ahead, offset)
		}
	}
	return ahead
}

func PrimeBars(instances []Instance) int {
	bars := 0
	for _, instance := range instances {
		bars = max(bars, primeDepth(instance.Indicator))
	}
	return min(bars, MaxPrimeBars)
}

func primeDepth(ind Indicator) int {
	if primer, ok := ind.(Primer); ok {
		return primer.PrimeBars()
	}
	return ind.Warmup()
}

func (s Spec) key(params Params) string {
	parts := make([]string, 0, len(s.Params)+2)
	parts = append(parts, s.Name)

	for _, param := range s.Params {
		parts = append(parts, param.format(params.values[param.Name]))
	}
	if s.Sourced && params.Source() != DefaultSource {
		parts = append(parts, sourceParam+"="+params.Source().String())
	}

	return strings.Join(parts, ":")
}

func (s Spec) arity() string {
	switch len(s.Params) {
	case 0:
		return "no parameters"
	case 1:
		return "1 parameter"
	default:
		return strconv.Itoa(len(s.Params)) + " parameters"
	}
}
