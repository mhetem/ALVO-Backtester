package market

import (
	"slices"
	"testing"
	"testing/fstest"
)

func jsonFile(body string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(body)}
}

func TestLoadIndexListsKeepsTheNewestFilePerIndex(t *testing.T) {
	fsys := fstest.MapFS{
		"indexes/ibov-2026-01.json": jsonFile(`{"index":"ibov","as_of":"2026-01-05","tickers":["PETR4","VALE3"]}`),
		"indexes/ibov-2026-05.json": jsonFile(`{"index":"ibov","as_of":"2026-05-04","tickers":["PETR4","ITUB4"]}`),
		"indexes/ibov-2025-09.json": jsonFile(`{"index":"ibov","as_of":"2025-09-01","tickers":["OLDD3"]}`),
		"indexes/smll-2026-01.json": jsonFile(`{"index":"smll","as_of":"2026-01-05","tickers":["mglu3"]}`),
	}

	lists, err := LoadIndexLists(fsys, "indexes")
	if err != nil {
		t.Fatalf("LoadIndexLists: %v", err)
	}

	if len(lists) != 2 {
		t.Fatalf("got %d lists, want 2", len(lists))
	}
	if lists[0].Index != "ibov" || lists[1].Index != "smll" {
		t.Fatalf("lists were %s, %s; want ibov, smll", lists[0].Index, lists[1].Index)
	}
	if lists[0].AsOf != "2026-05-04" {
		t.Errorf("ibov resolved to %s, want the 2026-05 file", lists[0].AsOf)
	}
	if !slices.Equal(lists[1].Tickers, []string{"MGLU3"}) {
		t.Errorf("smll tickers were %v, want [MGLU3] uppercased", lists[1].Tickers)
	}

	if union := UnionTickers(lists); !slices.Equal(union, []string{"ITUB4", "MGLU3", "PETR4"}) {
		t.Errorf("union was %v", union)
	}
}

func TestLoadIndexListsRejectsBadFiles(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"filename without a date": {
			"indexes/ibov.json": jsonFile(`{"index":"ibov","as_of":"2026-05-04","tickers":["PETR4"]}`),
		},
		"index disagrees with filename": {
			"indexes/ibov-2026-05.json": jsonFile(`{"index":"ibrx","as_of":"2026-05-04","tickers":["PETR4"]}`),
		},
		"month out of range": {
			"indexes/ibov-2026-13.json": jsonFile(`{"index":"ibov","as_of":"2026-05-04","tickers":["PETR4"]}`),
		},
		"duplicate ticker": {
			"indexes/ibov-2026-05.json": jsonFile(`{"index":"ibov","as_of":"2026-05-04","tickers":["PETR4","petr4"]}`),
		},
		"empty tickers": {
			"indexes/ibov-2026-05.json": jsonFile(`{"index":"ibov","as_of":"2026-05-04","tickers":[]}`),
		},
		"as_of is not a date": {
			"indexes/ibov-2026-05.json": jsonFile(`{"index":"ibov","as_of":"May 2026","tickers":["PETR4"]}`),
		},
		"misspelled field": {
			"indexes/ibov-2026-05.json": jsonFile(`{"index":"ibov","as_of":"2026-05-04","ticker":["PETR4"]}`),
		},
	}

	for name, fsys := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadIndexLists(fsys, "indexes"); err == nil {
				t.Error("LoadIndexLists accepted the file, want an error")
			}
		})
	}
}

func TestLoadIndexListsRequiresAtLeastOneFile(t *testing.T) {
	fsys := fstest.MapFS{"indexes/README.txt": jsonFile("not json")}

	if _, err := LoadIndexLists(fsys, "indexes"); err == nil {
		t.Error("LoadIndexLists accepted an empty directory, want an error")
	}
}
