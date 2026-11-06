// Package ingest loads CSV/JSON files into validated, de-duplicated rows.
//
// Idempotency: rows are keyed by RowKey(). The first occurrence is kept; later copies are
// reported in Result.Duplicates and never counted, so loading a file, or the same file
// concatenated with itself, yields the same rows. Invalid rows are collected in Result.Errors
// with their position instead of being silently dropped, so callers decide whether to go on.
package ingest

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jegorscode/reservelint/internal/model"
)

// ErrFile marks a problem with a file as a whole (unreadable, empty, wrong shape or columns).
var ErrFile = errors.New("unusable input file")

// RowError is an invalid row. Position is the CSV line number or the 1-based JSON item index.
type RowError struct {
	Position int
	Message  string
}

// Duplicate records rows whose key was already seen.
type Duplicate struct {
	Key             string
	FirstPosition   int
	RepeatPositions []int
}

// Result is the outcome of loading one file.
type Result[T model.Keyed] struct {
	Path       string
	Rows       []T // validated, de-duplicated, in file order
	Duplicates []Duplicate
	Errors     []RowError
	RowsRead   int // data rows seen in the file, valid or not
}

// DuplicateCount is the number of repeated rows that were ignored.
func (r *Result[T]) DuplicateCount() int {
	n := 0
	for _, d := range r.Duplicates {
		n += len(d.RepeatPositions)
	}
	return n
}

// ErrorOrNil returns an error describing the invalid rows, or nil if there are none.
func (r *Result[T]) ErrorOrNil() error {
	if len(r.Errors) == 0 {
		return nil
	}
	first := r.Errors[0]
	return fmt.Errorf("%s: %d invalid row(s); first at %d: %s", r.Path, len(r.Errors), first.Position, first.Message)
}

// LoadReserves loads a reserves snapshot.
func LoadReserves(path string) (*Result[model.ReserveRow], error) {
	return load(path, model.ReserveRequired, model.ParseReserve)
}

// LoadSupply loads circulating-supply snapshots.
func LoadSupply(path string) (*Result[model.SupplyRow], error) {
	return load(path, model.SupplyRequired, model.ParseSupply)
}

// LoadRedemptions loads the redemption log.
func LoadRedemptions(path string) (*Result[model.RedemptionRow], error) {
	return load(path, model.RedemptionRequired, model.ParseRedemption)
}

// LatestSupply returns the supply snapshot with the latest as_of.
func LatestSupply(rows []model.SupplyRow) (model.SupplyRow, error) {
	if len(rows) == 0 {
		return model.SupplyRow{}, errors.New("no supply rows")
	}
	best := rows[0]
	for _, r := range rows[1:] {
		if r.AsOf.After(best.AsOf) {
			best = r
		}
	}
	return best, nil
}

type rawRecord struct {
	pos   int
	cells map[string]string
	err   error // set when the row could not even be split into cells
}

func fileErr(path, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", ErrFile, path, fmt.Sprintf(format, args...))
}

func load[T model.Keyed](path string, required []string, parse func(map[string]string) (T, error)) (*Result[T], error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fileErr(path, "cannot read file: %v", err)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	var raw []rawRecord
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		raw, err = readCSV(path, data, required)
	case ".json":
		raw, err = readJSON(path, data)
	default:
		err = fileErr(path, "unsupported file type %q (use .csv or .json)", filepath.Ext(path))
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fileErr(path, "file contains no data rows")
	}

	res := &Result[T]{Path: path, RowsRead: len(raw)}
	first := map[string]int{}
	repeats := map[string][]int{}
	var order []string
	for _, rec := range raw {
		if rec.err != nil {
			res.Errors = append(res.Errors, RowError{rec.pos, rec.err.Error()})
			continue
		}
		row, err := parse(rec.cells)
		if err != nil {
			res.Errors = append(res.Errors, RowError{rec.pos, strings.ReplaceAll(err.Error(), "\n", "; ")})
			continue
		}
		key := row.RowKey()
		if _, seen := first[key]; seen {
			if len(repeats[key]) == 0 {
				order = append(order, key)
			}
			repeats[key] = append(repeats[key], rec.pos)
			continue
		}
		first[key] = rec.pos
		res.Rows = append(res.Rows, row)
	}
	for _, key := range order {
		res.Duplicates = append(res.Duplicates, Duplicate{key, first[key], repeats[key]})
	}
	return res, nil
}

func readCSV(path string, data []byte, required []string) ([]rawRecord, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, fileErr(path, "file contains no data rows")
	}
	if err != nil {
		return nil, fileErr(path, "bad header: %v", err)
	}
	for i := range header {
		header[i] = strings.TrimSpace(header[i])
	}
	have := map[string]bool{}
	for _, h := range header {
		have[h] = true
	}
	var missing []string
	for _, c := range required {
		if !have[c] {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fileErr(path, "missing required column(s): %s", strings.Join(missing, ", "))
	}

	var out []rawRecord
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		var pe *csv.ParseError
		if errors.As(err, &pe) {
			out = append(out, rawRecord{pos: pe.StartLine, err: pe.Err})
			continue
		}
		if err != nil {
			return nil, fileErr(path, "%v", err)
		}
		line, _ := r.FieldPos(0)
		switch {
		case len(rec) > len(header):
			out = append(out, rawRecord{pos: line, err: errors.New("row has more cells than header columns")})
		case len(rec) < len(header):
			out = append(out, rawRecord{pos: line, err: errors.New("row has fewer cells than header columns")})
		default:
			cells := make(map[string]string, len(header))
			for i, h := range header {
				cells[h] = rec[i]
			}
			out = append(out, rawRecord{pos: line, cells: cells})
		}
	}
}

func readJSON(path string, data []byte) ([]rawRecord, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // keep numbers as written; never round-trip money through float64
	var items []json.RawMessage
	if err := dec.Decode(&items); err != nil {
		return nil, fileErr(path, "JSON must be a list of objects: %v", err)
	}
	out := make([]rawRecord, 0, len(items))
	for i, item := range items {
		rec := rawRecord{pos: i + 1}
		var obj map[string]any
		d := json.NewDecoder(bytes.NewReader(item))
		d.UseNumber()
		if err := d.Decode(&obj); err != nil || obj == nil {
			rec.err = errors.New("row is not an object")
			out = append(out, rec)
			continue
		}
		rec.cells = make(map[string]string, len(obj))
		for k, v := range obj {
			switch x := v.(type) {
			case nil:
				rec.cells[k] = ""
			case string:
				rec.cells[k] = x
			case json.Number:
				rec.cells[k] = x.String()
			case bool:
				rec.cells[k] = fmt.Sprint(x)
			default:
				rec.err = fmt.Errorf("%s: unsupported JSON value (use text, number or boolean)", k)
			}
		}
		out = append(out, rec)
	}
	return out, nil
}
