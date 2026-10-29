package windowing

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func row(id, store, minute string, amount int64) string {
	return fmt.Sprintf(`{"event_id":%q,"store":%q,"occurred_at":"2026-10-29T10:%s:00Z","amount_minor":%d}`+"\n", id, store, minute, amount)
}
func execute(t *testing.T, input string, c Config) Report {
	t.Helper()
	got, err := Run(strings.NewReader(input), c)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func TestWatermarkBoundaryAndDuplicate(t *testing.T) {
	input := row("a", "NORTH", "01", 100) + row("b", "NORTH", "07", 200) + row("c", "NORTH", "04", 300) + row("a", "NORTH", "01", 100)
	got := execute(t, input, DefaultConfig())
	if got.Summary != (Summary{Rows: 4, Accepted: 2, Duplicates: 1, Late: 1}) {
		t.Fatalf("summary: %+v", got.Summary)
	}
	if len(got.Windows) != 2 || got.Windows[0].Amount != 100 || got.Windows[0].Status != "watermarked" || got.Windows[1].Status != "eof_preview" {
		t.Fatalf("windows: %+v", got.Windows)
	}
	if got.Trace[0].Watermark.Format("15:04") != "09:59" || got.Trace[1].Watermark.Format("15:04") != "10:05" {
		t.Fatal("trace overwritten")
	}
}
func TestBeforeBoundaryIsStillOpen(t *testing.T) {
	got := execute(t, row("a", "NORTH", "01", 100)+row("b", "NORTH", "06", 200)+row("c", "NORTH", "04", 300), DefaultConfig())
	if got.Summary.Accepted != 3 || got.Summary.Late != 0 || got.Windows[0].Amount != 400 {
		t.Fatalf("%+v", got)
	}
}
func TestZeroLag(t *testing.T) {
	c := DefaultConfig()
	c.Lag = 0
	got := execute(t, row("a", "NORTH", "05", 100)+row("b", "NORTH", "04", 200), c)
	if got.Summary.Late != 1 || !got.Windows[0].Start.Equal(time.Date(2026, 10, 29, 10, 5, 0, 0, time.UTC)) {
		t.Fatal(got)
	}
}
func TestInvalidFutureDoesNotAdvance(t *testing.T) {
	bad := `{"event_id":"bad","store":"NORTH","occurred_at":"2099-01-01T00:00:00Z","amount_minor":-1}` + "\n"
	got := execute(t, row("a", "NORTH", "01", 100)+bad+row("b", "NORTH", "02", 200), DefaultConfig())
	if got.Summary.Invalid != 1 || got.Summary.Accepted != 2 || got.Watermark.Format("15:04") != "10:00" {
		t.Fatal(got)
	}
}
func TestLateIDRemainsSeen(t *testing.T) {
	input := row("a", "NORTH", "01", 100) + row("b", "NORTH", "08", 200) + row("c", "NORTH", "03", 300)
	got := execute(t, input+row("c", "NORTH", "03", 300), DefaultConfig())
	if got.Summary.Late != 1 || got.Summary.Duplicates != 1 {
		t.Fatal(got.Summary)
	}
	if _, err := Run(strings.NewReader(input+row("c", "NORTH", "03", 301)), DefaultConfig()); err == nil {
		t.Fatal("expected conflict after late")
	}
}
func TestConflictIsFatal(t *testing.T) {
	got, err := Run(strings.NewReader(row("a", "NORTH", "01", 100)+row("a", "SOUTH", "01", 100)), DefaultConfig())
	if err == nil || got.SchemaVersion != 0 {
		t.Fatal("partial result escaped")
	}
}
func TestReplayAndSorting(t *testing.T) {
	input := row("s", "SOUTH", "03", 30) + row("n", "NORTH", "02", 20) + row("m", "NORTH", "01", 10)
	a := execute(t, input, DefaultConfig())
	b := execute(t, input, DefaultConfig())
	if !reflect.DeepEqual(a, b) {
		t.Fatal("non-deterministic replay")
	}
	if a.Windows[0].Store != "NORTH" || a.Windows[0].Count != 2 || a.Windows[0].Amount != 30 || a.Windows[1].Store != "SOUTH" {
		t.Fatal(a.Windows)
	}
}
func TestLargeLagAgainstIndependentTotals(t *testing.T) {
	c := DefaultConfig()
	c.Lag = 24 * time.Hour
	input := ""
	expected := map[string]int64{"NORTH": 0, "SOUTH": 0}
	// Я считаю контрольные суммы отдельно от алгоритма окон.
	for i := 0; i < 120; i++ {
		store := []string{"NORTH", "SOUTH"}[i%2]
		amount := int64(i + 1)
		s := row(fmt.Sprintf("e%d", i), store, fmt.Sprintf("%02d", (i*17)%60), amount)
		input += s + s
		expected[store] += amount
	}
	got := execute(t, input, c)
	actual := map[string]int64{}
	count := 0
	for _, w := range got.Windows {
		actual[w.Store] += w.Amount
		count += w.Count
		if w.Status != "eof_preview" {
			t.Fatal(w.Status)
		}
	}
	if !reflect.DeepEqual(actual, expected) || count != 120 || got.Summary.Duplicates != 120 || got.Summary.Late != 0 {
		t.Fatal(actual, expected, got.Summary)
	}
}
func TestResourceLimits(t *testing.T) {
	cases := []struct {
		name  string
		c     Config
		input string
	}{
		{"rows", Config{time.Minute, 0, 1, 10}, row("a", "NORTH", "01", 1) + row("a", "NORTH", "01", 1)},
		{"groups", Config{time.Minute, time.Hour, 10, 1}, row("a", "NORTH", "01", 1) + row("b", "SOUTH", "01", 1)},
		{"oversize", DefaultConfig(), strings.Repeat("x", 65536)},
		{"empty", DefaultConfig(), ""},
		{"all-invalid", DefaultConfig(), "{}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Run(strings.NewReader(tc.input), tc.c)
			if err == nil || got.SchemaVersion != 0 {
				t.Fatal("expected fatal without partial report")
			}
		})
	}
}
func TestConfigBounds(t *testing.T) {
	for _, c := range []Config{{0, 0, 1, 1}, {7 * time.Minute, 0, 1, 1}, {time.Minute, -1, 1, 1}, {time.Minute, 25 * time.Hour, 1, 1}, {time.Minute, 0, 1000001, 1}, {time.Minute, 0, 1, 0}} {
		if c.Validate() == nil {
			t.Fatal(c)
		}
	}
	if (Config{time.Second, 24 * time.Hour, 1000000, 100000}).Validate() != nil {
		t.Fatal("valid upper bounds")
	}
}

type brokenReader struct{ sent bool }

func (b *brokenReader) Read(p []byte) (int, error) {
	if b.sent {
		return 0, errors.New("synthetic read failure")
	}
	b.sent = true
	return copy(p, row("a", "NORTH", "01", 1)), nil
}
func TestReadFailureDoesNotReturnPartial(t *testing.T) {
	got, err := Run(&brokenReader{}, DefaultConfig())
	if err == nil || got.SchemaVersion != 0 {
		t.Fatal("expected read failure")
	}
}
func TestTimestampMidnightAndMoneyBoundary(t *testing.T) {
	input := `{"event_id":"a","store":"NORTH","occurred_at":"2026-10-28T23:59:59Z","amount_minor":1000000000000}` + "\n" + `{"event_id":"b","store":"NORTH","occurred_at":"2026-10-29T00:01:00Z","amount_minor":1000000000000}` + "\n"
	got := execute(t, input, DefaultConfig())
	if len(got.Windows) != 2 || got.Windows[0].End.Format(time.RFC3339) != "2026-10-29T00:00:00Z" || got.Windows[0].Amount != 1000000000000 {
		t.Fatal(got.Windows)
	}
}
func TestInvalidContracts(t *testing.T) {
	valid := strings.TrimSpace(row("a", "NORTH", "01", 1))
	cases := map[string]string{
		"array": "[]", "blank": "", "malformed": "{", "trailing": valid + " {}",
		"missing": `{"event_id":"a"}`, "unknown": strings.Replace(valid, `"store"`, `"stores"`, 1),
		"key-case":         strings.Replace(valid, `"store"`, `"Store"`, 1),
		"duplicate-key":    strings.Replace(valid, `"store":"NORTH"`, `"store":"NORTH","store":"SOUTH"`, 1),
		"escape-duplicate": strings.Replace(valid, `"store":"NORTH"`, `"store":"NORTH","\u0073tore":"SOUTH"`, 1),
		"null":             strings.Replace(valid, `"amount_minor":1`, `"amount_minor":null`, 1),
		"float":            strings.Replace(valid, `"amount_minor":1`, `"amount_minor":1.5`, 1),
		"string-number":    strings.Replace(valid, `"amount_minor":1`, `"amount_minor":"1"`, 1),
		"zero":             strings.Replace(valid, `"amount_minor":1`, `"amount_minor":0`, 1),
		"negative":         strings.Replace(valid, `"amount_minor":1`, `"amount_minor":-1`, 1),
		"over-money":       strings.Replace(valid, `"amount_minor":1`, `"amount_minor":1000000000001`, 1),
		"overflow":         strings.Replace(valid, `"amount_minor":1`, `"amount_minor":9223372036854775808`, 1),
		"exponent":         strings.Replace(valid, `"amount_minor":1`, `"amount_minor":1e3`, 1),
		"bad-store":        strings.Replace(valid, "NORTH", "north", 1), "bad-id": strings.Replace(valid, `"a"`, `"a b"`, 1),
		"bad-date": strings.Replace(valid, "2026-10-29", "2026-02-30", 1),
		"offset":   strings.Replace(valid, "00Z", "00+00:00", 1), "fraction": strings.Replace(valid, "00Z", "00.5Z", 1),
		"old-year": strings.Replace(valid, "2026", "2019", 1), "future-year": strings.Replace(valid, "2026", "2100", 1),
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			if _, code := parseEvent([]byte(line)); code == "" {
				t.Fatal("accepted invalid contract")
			}
		})
	}
	if _, code := parseEvent([]byte(valid)); code != "" {
		t.Fatal(code)
	}
}
func TestReadWithoutFinalNewline(t *testing.T) {
	got := execute(t, strings.TrimSpace(row("a", "NORTH", "01", 1)), DefaultConfig())
	if got.Summary.Rows != 1 {
		t.Fatal(got)
	}
}

var _ io.Reader = (*brokenReader)(nil)
