// Package synthetic generates fictional sample data, including deliberate violations.
//
// Output is fully determined by the seed. Thresholds are NOT referenced here: the violations
// are large enough to trip any plausible threshold. The Go generator uses its own random
// stream, so its files differ from the Python prototype's; cross-implementation parity is
// tested against testdata/vectors instead.
package synthetic

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jegorscode/reservelint/internal/meta"
	"github.com/jegorscode/reservelint/internal/model"
	"github.com/shopspring/decimal"
)

// Format is the output file format.
type Format string

// Supported formats.
const (
	CSV  Format = "csv"
	JSON Format = "json"
)

// Scenario names.
const (
	Clean      = "clean"
	Violations = "violations"
)

var (
	// AsOf is the fixed reference time of all generated data (a Monday).
	AsOf = time.Date(2027, 2, 1, 21, 0, 0, 0, time.UTC)

	counterparties = []string{
		"Synthetic Bank Alpha", "Synthetic Bank Beta", "Synthetic Custody Gamma",
		"Synthetic Trust Delta", "Synthetic Bank Epsilon",
	}
)

const (
	supplyCents  = int64(100_000_000) * 100
	nRedemptions = 60
	hoursPerDay  = 24
)

// Data is one scenario's three datasets, as the CSV/JSON cell text of each row.
type Data struct {
	Reserves    []model.ReserveRow
	Supply      []model.SupplyRow
	Redemptions []model.RedemptionRow
}

func money(cents int64) decimal.Decimal { return decimal.New(cents, -2) }

func newRNG(seed int64, name string) *rand.Rand {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return rand.New(rand.NewPCG(uint64(seed), h.Sum64()))
}

func uniform(r *rand.Rand, lo, hi float64) float64 { return lo + r.Float64()*(hi-lo) }

func days(d float64) time.Duration {
	return time.Duration(d * hoursPerDay * float64(time.Hour))
}

func split(total int64, weights []float64) []int64 {
	var sum float64
	for _, w := range weights {
		sum += w
	}
	parts := make([]int64, len(weights))
	var used int64
	for i, w := range weights {
		parts[i] = int64(float64(total) * w / sum)
		used += parts[i]
	}
	parts[len(parts)-1] += total - used
	return parts
}

func buildReserves(r *rand.Rand, totalCents int64, weights []float64, cashFraction float64) []model.ReserveRow {
	var rows []model.ReserveRow
	shares := split(totalCents, weights)
	for i, cp := range counterparties {
		cpCents := shares[i]
		cash := int64(float64(cpCents) * cashFraction)
		billA := int64(float64(cpCents-cash) * uniform(r, 0.3, 0.7))
		billB := cpCents - cash - billA
		rows = append(rows, model.ReserveRow{
			AssetType: "cash_deposit", Amount: money(cash), Counterparty: cp, IsImmediatelyAvailable: true,
		})
		for _, cents := range []int64{billA, billB} {
			today := time.Date(AsOf.Year(), AsOf.Month(), AsOf.Day(), 0, 0, 0, 0, time.UTC)
			maturity := today.AddDate(0, 0, 7+r.IntN(84))
			rows = append(rows, model.ReserveRow{
				AssetType: "us_treasury_bill", Amount: money(cents), Counterparty: cp, MaturityDate: &maturity,
			})
		}
	}
	return rows
}

func buildRedemptions(r *rand.Rand, violations bool) []model.RedemptionRow {
	rows := make([]model.RedemptionRow, 0, nRedemptions)
	for i := 0; i < nRedemptions; i++ {
		request := AsOf.Add(-days(uniform(r, 0.2, 30)))
		// Causality: onboarding strictly precedes the request, which strictly precedes any
		// transfer (delays below are at least an hour, onboarding at least a day earlier).
		onboarded := request.Add(-days(uniform(r, 1, 400)))
		delay := days(uniform(r, 0.05, 1.2))
		stuck := false
		if violations && i%7 == 3 {
			delay = days(uniform(r, 4, 9))
		}
		if violations && (i == 5 || i == 11) { // old request, still no transfer
			request, stuck = AsOf.Add(-12*hoursPerDay*time.Hour), true
			onboarded = request.Add(-30 * hoursPerDay * time.Hour)
		}
		request, onboarded = request.Truncate(time.Second), onboarded.Truncate(time.Second)
		transfer := request.Add(delay).Truncate(time.Second)
		pending := stuck || transfer.After(AsOf)
		row := model.RedemptionRow{
			RequestTS:   request,
			OnboardedTS: &onboarded,
			Amount:      money(10_000*100 + r.Int64N(50_000_000*100-10_000*100)),
			Status:      model.StatusCompleted,
		}
		if pending {
			row.Status = model.StatusPending
		} else {
			row.TransferInitiatedTS = &transfer
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].RequestTS.Before(rows[j].RequestTS) })
	return rows
}

// Build returns the datasets of a scenario ("clean" or "violations") for a seed.
func Build(name string, seed int64) (Data, error) {
	r := newRNG(seed, name)
	var d Data
	switch name {
	case Clean:
		weights := make([]float64, len(counterparties))
		for i := range weights {
			weights[i] = uniform(r, 0.85, 1.15)
		}
		d.Reserves = buildReserves(r, supplyCents*102/100, weights, 0.20)
	case Violations:
		d.Reserves = buildReserves(r, supplyCents*95/100, []float64{5.5, 1, 1, 1, 1.5}, 0.02)
		d.Reserves = append(d.Reserves, d.Reserves[0]) // exact duplicate of a small cash row
	default:
		return Data{}, fmt.Errorf("unknown scenario %q", name)
	}
	d.Supply = []model.SupplyRow{{AsOf: AsOf, CirculatingSupply: money(supplyCents)}}
	d.Redemptions = buildRedemptions(r, name == Violations)
	return d, nil
}

// ---- writing ---------------------------------------------------------------------------

type table struct {
	header []string
	rows   [][]string
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func optTS(t *time.Time) string {
	if t == nil {
		return ""
	}
	return ts(*t)
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func (d Data) tables() map[string]table {
	res := table{header: []string{"asset_type", "amount", "counterparty", "maturity_date", "is_immediately_available"}}
	for _, r := range d.Reserves {
		maturity := ""
		if r.MaturityDate != nil {
			maturity = r.MaturityDate.Format("2006-01-02")
		}
		res.rows = append(res.rows, []string{r.AssetType, r.Amount.StringFixed(2), r.Counterparty, maturity, boolText(r.IsImmediatelyAvailable)})
	}
	sup := table{header: []string{"as_of", "circulating_supply"}}
	for _, r := range d.Supply {
		sup.rows = append(sup.rows, []string{ts(r.AsOf), r.CirculatingSupply.StringFixed(2)})
	}
	red := table{header: []string{"request_ts", "onboarded_ts", "transfer_initiated_ts", "amount", "status"}}
	for _, r := range d.Redemptions {
		red.rows = append(red.rows, []string{ts(r.RequestTS), optTS(r.OnboardedTS), optTS(r.TransferInitiatedTS), r.Amount.StringFixed(2), string(r.Status)})
	}
	return map[string]table{"reserves": res, "supply": sup, "redemptions": red}
}

func writeTable(path string, t table, f Format) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if f == JSON {
		objs := make([]map[string]string, 0, len(t.rows))
		for _, row := range t.rows {
			obj := map[string]string{}
			for i, h := range t.header {
				if row[i] != "" {
					obj[h] = row[i]
				}
			}
			objs = append(objs, obj)
		}
		enc := json.NewEncoder(file)
		enc.SetIndent("", "  ")
		return enc.Encode(objs)
	}
	w := csv.NewWriter(file)
	if err := w.Write(t.header); err != nil {
		return err
	}
	if err := w.WriteAll(t.rows); err != nil {
		return err
	}
	return w.Error()
}

var manifestNotes = map[string][]string{
	Clean: {"No deliberate violations."},
	Violations: {
		"Reserves are below circulating supply (coverage shortfall).",
		"Very little of the reserve pool is flagged immediately available.",
		"One counterparty holds more than half of reserves.",
		"Several redemptions took many days; two old requests have no transfer yet.",
		"One reserve row appears twice (duplicate detection).",
	},
	"both": {"Onboarding precedes every request, and every transfer follows its request."},
}

// Generate writes both scenarios plus manifest.json under outDir and returns the paths written.
func Generate(outDir string, seed int64, f Format) ([]string, error) {
	if f != CSV && f != JSON {
		return nil, fmt.Errorf("unknown format %q", f)
	}
	var written []string
	for _, name := range []string{Clean, Violations} {
		data, err := Build(name, seed)
		if err != nil {
			return nil, err
		}
		dir := filepath.Join(outDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		tables := data.tables()
		for _, kind := range []string{"reserves", "supply", "redemptions"} {
			path := filepath.Join(dir, kind+"."+string(f))
			if err := writeTable(path, tables[kind], f); err != nil {
				return nil, err
			}
			written = append(written, path)
		}
	}
	manifest := map[string]any{
		"synthetic":         true,
		"disclaimer":        meta.Disclaimer,
		"tool_version":      meta.Version,
		"seed":              seed,
		"as_of":             AsOf.Format(time.RFC3339),
		"deliberate_issues": manifestNotes,
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(outDir, "manifest.json")
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return nil, err
	}
	return append(written, path), nil
}
