// Package ingest loads CSV/JSON files into validated rows and applies explicit duplicate rules.
//
// Reserve positions can legitimately repeat: two cash deposits at one bank can share every
// field. So identity depends on an optional holding_id:
//
//   - With a holding_id: the id is part of the row key. A line repeated exactly (same id, same
//     content) is a true duplicate: it is dropped and reported, which keeps loading idempotent.
//     The same id with different content is an error, never a silent choice.
//   - Without a holding_id: identical lines are ambiguous. They are KEPT as separate positions
//     (never silently dropped) and reported in Result.Ambiguous so callers can warn.
//     Options.DropIdenticalUnkeyed restores the old drop-and-report behaviour.
//
// Supply and redemption rows have no ids: exact repeats are dropped and reported. Invalid rows
// are collected in Result.Errors with their position, so callers decide whether to go on.
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

	"github.com/jegorscode/reservelint/internal/config"
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
	// RepeatRowIndexes are, for Result.Ambiguous groups only, the indexes in Result.Rows of the
	// repeated rows that were kept.
	RepeatRowIndexes []int
}

// Options tunes duplicate handling.
type Options struct {
	// DropIdenticalUnkeyed drops identical rows that have no holding_id (reporting them in
	// Result.Duplicates) instead of keeping them as ambiguous separate positions. This is the
	// pre-holding_id behaviour; it understates reserves if the rows were distinct positions.
	DropIdenticalUnkeyed bool
}

// Result is the outcome of loading one file.
type Result[T model.Keyed] struct {
	Path       string
	SHA256     string      // hash of the exact file bytes that were parsed
	Rows       []T         // validated rows in file order, without true duplicates
	Duplicates []Duplicate // exact repeats that were dropped
	// Ambiguous lists identical rows without a holding_id that were kept as separate positions.
	Ambiguous []Duplicate
	Errors    []RowError
	RowsRead  int // data rows seen in the file, valid or not
}

// AmbiguousCount is the number of repeated unkeyed rows that were kept.
func (r *Result[T]) AmbiguousCount() int {
	n := 0
	for _, d := range r.Ambiguous {
		n += len(d.RepeatPositions)
	}
	return n
}

// DuplicateCount is the number of repeated rows that were dropped.
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
	return LoadReservesWith(path, Options{})
}

// LoadReservesWith loads a reserves snapshot with explicit duplicate options.
func LoadReservesWith(path string, opts Options) (*Result[model.ReserveRow], error) {
	return load(path, model.ReserveRequired, model.ParseReserve, identity[model.ReserveRow]{
		holdingID:   func(r model.ReserveRow) string { return r.HoldingID },
		keepUnkeyed: !opts.DropIdenticalUnkeyed,
	})
}

// LoadSupply loads circulating-supply snapshots.
func LoadSupply(path string) (*Result[model.SupplyRow], error) {
	return load(path, model.SupplyRequired, model.ParseSupply, identity[model.SupplyRow]{})
}

// LoadRedemptions loads the redemption log.
func LoadRedemptions(path string) (*Result[model.RedemptionRow], error) {
	return load(path, model.RedemptionRequired, model.ParseRedemption, identity[model.RedemptionRow]{})
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

// identity describes how a row type is identified. The zero value means: rows have no id and
// exact repeats are dropped.
type identity[T any] struct {
	holdingID   func(T) string // nil when the type has no ids
	keepUnkeyed bool           // keep identical rows that have no id (ambiguous) instead of dropping
}

// dupSet collects repeat groups in first-seen order.
type dupSet struct {
	order []string
	byKey map[string]*Duplicate
}

func (d *dupSet) add(key string, first, pos, rowIndex int, withIndex bool) {
	if d.byKey == nil {
		d.byKey = map[string]*Duplicate{}
	}
	g, ok := d.byKey[key]
	if !ok {
		g = &Duplicate{Key: key, FirstPosition: first}
		d.byKey[key] = g
		d.order = append(d.order, key)
	}
	g.RepeatPositions = append(g.RepeatPositions, pos)
	if withIndex {
		g.RepeatRowIndexes = append(g.RepeatRowIndexes, rowIndex)
	}
}

func (d *dupSet) list() []Duplicate {
	var out []Duplicate
	for _, k := range d.order {
		out = append(out, *d.byKey[k])
	}
	return out
}

func load[T model.Keyed](path string, required []string, parse func(map[string]string) (T, error), id identity[T]) (*Result[T], error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fileErr(path, "cannot read file: %v", err)
	}
	sum := config.HashBytes(data) // hash of the file exactly as stored, BOM included
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

	res := &Result[T]{Path: path, SHA256: sum, RowsRead: len(raw)}
	type seenID struct {
		key string
		pos int
	}
	first := map[string]int{} // row key -> position of first occurrence
	ids := map[string]seenID{}
	var dropped, ambiguous dupSet
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
		hid := ""
		if id.holdingID != nil {
			hid = id.holdingID(row)
		}
		switch {
		case hid != "":
			if prev, ok := ids[hid]; ok {
				if prev.key != key {
					res.Errors = append(res.Errors, RowError{rec.pos, fmt.Sprintf("holding_id %q already used at %d with different content", hid, prev.pos)})
				} else {
					dropped.add(key, prev.pos, rec.pos, 0, false) // same id, same content: a repeated line
				}
				continue
			}
			ids[hid] = seenID{key, rec.pos}
			first[key] = rec.pos
		case first[key] != 0:
			if id.keepUnkeyed {
				ambiguous.add(key, first[key], rec.pos, len(res.Rows), true)
				break // fall through to append: the repeat is kept as its own position
			}
			dropped.add(key, first[key], rec.pos, 0, false)
			continue
		default:
			first[key] = rec.pos
		}
		res.Rows = append(res.Rows, row)
	}
	res.Duplicates, res.Ambiguous = dropped.list(), ambiguous.list()
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
