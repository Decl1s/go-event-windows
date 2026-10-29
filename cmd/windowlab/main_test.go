package main

import (
	"bytes"
	"encoding/json"
	"github.com/Decl1s/go-event-windows/internal/windowing"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func invoke(t *testing.T, args ...string) int {
	t.Helper()
	var out, err bytes.Buffer
	return run(args, &out, &err)
}
func TestDemoAndRepeat(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "events.jsonl")
	output := filepath.Join(dir, "report.json")
	if invoke(t, "demo", "-out", input) != 0 || invoke(t, "run", "-in", input, "-out", output) != 0 {
		t.Fatal("demo failed")
	}
	original, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var report windowing.Report
	if json.Unmarshal(original, &report) != nil {
		t.Fatal("bad JSON")
	}
	if report.Summary != (windowing.Summary{Rows: 11, Accepted: 6, Duplicates: 2, Late: 2, Invalid: 1}) {
		t.Fatal(report.Summary)
	}
	amounts := []int64{}
	statuses := []string{}
	for _, w := range report.Windows {
		amounts = append(amounts, w.Amount)
		statuses = append(statuses, w.Status)
	}
	if !reflect.DeepEqual(amounts, []int64{30000, 7000, 35000, 11000}) || !reflect.DeepEqual(statuses, []string{"watermarked", "watermarked", "watermarked", "eof_preview"}) {
		t.Fatal(report.Windows)
	}
	if invoke(t, "run", "-in", input, "-out", output) == 0 {
		t.Fatal("overwrote report")
	}
	after, _ := os.ReadFile(output)
	if !bytes.Equal(original, after) {
		t.Fatal("changed report")
	}
	second := filepath.Join(dir, "second.json")
	if invoke(t, "run", "-in", input, "-out", second) != 0 {
		t.Fatal("repeat failed")
	}
	again, _ := os.ReadFile(second)
	if !bytes.Equal(original, again) {
		t.Fatal("repeat differs")
	}
	lagged := filepath.Join(dir, "lagged.json")
	if invoke(t, "run", "-in", input, "-out", lagged, "-lag", "10m") != 0 {
		t.Fatal("lag failed")
	}
	data, _ := os.ReadFile(lagged)
	json.Unmarshal(data, &report)
	total := int64(0)
	for _, w := range report.Windows {
		total += w.Amount
	}
	if report.Summary.Accepted != 8 || report.Summary.Late != 0 || total != 98000 {
		t.Fatal(report.Summary, total)
	}
}
func TestFatalHasNoOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input")
	output := filepath.Join(dir, "output")
	if err := os.WriteFile(input, []byte(demo), 0600); err != nil {
		t.Fatal(err)
	}
	if invoke(t, "run", "-in", input, "-out", output, "-max-rows", "2") == 0 {
		t.Fatal("expected fatal")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("partial output")
	}
}
func TestArgumentsAndMissingInput(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"demo"}, {"run", "-in", "missing"}, {"run", "-out", "unused"}, {"demo", "-out", "unused", "unexpected"}, {"run", "-in", "missing", "-out", "unused", "-lag", "-1s"}, {"run", "-in", "missing", "-out", "unused"}} {
		if invoke(t, args...) == 0 {
			t.Fatal(args)
		}
	}
	if invoke(t, "run", "-help") != 0 {
		t.Fatal("help failed")
	}
}
func TestDemoNeverReplacesInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input")
	os.WriteFile(path, []byte("keep"), 0600)
	if invoke(t, "demo", "-out", path) == 0 {
		t.Fatal("overwrote input")
	}
	got, _ := os.ReadFile(path)
	if strings.TrimSpace(string(got)) != "keep" {
		t.Fatal("lost input")
	}
}
