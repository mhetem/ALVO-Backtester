package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type IndexList struct {
	Index   string   `json:"index"`
	AsOf    string   `json:"as_of"`
	Source  string   `json:"source"`
	Tickers []string `json:"tickers"`

	File  string `json:"-"`
	Year  int    `json:"-"`
	Month int    `json:"-"`
}

var indexFilePattern = regexp.MustCompile(`^(.+)-(\d{4})-(\d{2})\.json$`)

func LoadIndexLists(fsys fs.FS, dir string) ([]IndexList, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	newest := map[string]IndexList{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		match := indexFilePattern.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil, fmt.Errorf("%s/%s: filename must be <index>-<YYYY>-<MM>.json", dir, entry.Name())
		}

		list, err := loadIndexList(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if list.Index != match[1] {
			return nil, fmt.Errorf("%s: index is %q but the filename says %q", list.File, list.Index, match[1])
		}

		year, _ := strconv.Atoi(match[2])
		month, _ := strconv.Atoi(match[3])
		if month < 1 || month > 12 {
			return nil, fmt.Errorf("%s: %q is not a month", list.File, match[3])
		}
		list.Year, list.Month = year, month

		current, seen := newest[list.Index]
		if !seen || year > current.Year || (year == current.Year && month > current.Month) {
			newest[list.Index] = list
		}
	}

	if len(newest) == 0 {
		return nil, fmt.Errorf("no index composition files in %s", dir)
	}

	lists := slices.Collect(maps.Values(newest))
	slices.SortFunc(lists, func(a, b IndexList) int { return strings.Compare(a.Index, b.Index) })
	return lists, nil
}

func loadIndexList(fsys fs.FS, name string) (IndexList, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return IndexList{}, fmt.Errorf("reading %s: %w", name, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var list IndexList
	if err := decoder.Decode(&list); err != nil {
		return IndexList{}, fmt.Errorf("parsing %s: %w", name, err)
	}
	list.File = name

	if list.Index == "" {
		return IndexList{}, fmt.Errorf("%s: index is required", name)
	}
	if _, err := time.Parse(time.DateOnly, list.AsOf); err != nil {
		return IndexList{}, fmt.Errorf("%s: as_of must be a YYYY-MM-DD date", name)
	}
	if len(list.Tickers) == 0 {
		return IndexList{}, fmt.Errorf("%s: tickers is empty", name)
	}

	seen := make(map[string]bool, len(list.Tickers))
	for i, ticker := range list.Tickers {
		normalised := strings.ToUpper(strings.TrimSpace(ticker))
		if normalised == "" {
			return IndexList{}, fmt.Errorf("%s: tickers[%d] is empty", name, i)
		}
		if seen[normalised] {
			return IndexList{}, fmt.Errorf("%s: duplicate ticker %q", name, normalised)
		}
		seen[normalised] = true
		list.Tickers[i] = normalised
	}

	return list, nil
}

func UnionTickers(lists []IndexList) []string {
	seen := map[string]bool{}
	union := []string{}

	for _, list := range lists {
		for _, ticker := range list.Tickers {
			if seen[ticker] {
				continue
			}
			seen[ticker] = true
			union = append(union, ticker)
		}
	}

	slices.Sort(union)
	return union
}
