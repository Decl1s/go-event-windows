// Package windowing — мой конечный replay одного упорядоченного по прибытиям JSONL.
// Здесь я изучаю event time, а не обещаю распределённый потоковый движок.
package windowing

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"time"
)

type Config struct {
	Window    time.Duration
	Lag       time.Duration
	MaxRows   int
	MaxGroups int
}

func DefaultConfig() Config { return Config{5 * time.Minute, 2 * time.Minute, 100000, 10000} }

func (c Config) Validate() error {
	if c.Window < time.Second || c.Window > time.Hour || c.Window%time.Second != 0 || 24*time.Hour%c.Window != 0 {
		return errors.New("я ожидаю window от 1s до 1h, делящее сутки без остатка")
	}
	if c.Lag < 0 || c.Lag > 24*time.Hour || c.Lag%time.Second != 0 {
		return errors.New("я ожидаю lag от 0s до 24h с целыми секундами")
	}
	if c.MaxRows < 1 || c.MaxRows > 1000000 {
		return errors.New("я ожидаю max-rows от 1 до 1000000")
	}
	if c.MaxGroups < 1 || c.MaxGroups > 100000 {
		return errors.New("я ожидаю max-groups от 1 до 100000")
	}
	return nil
}

type Event struct {
	ID     string    `json:"event_id"`
	Store  string    `json:"store"`
	At     time.Time `json:"occurred_at"`
	Amount int64     `json:"amount_minor"`
}

type Summary struct {
	Rows       int `json:"rows"`
	Accepted   int `json:"accepted"`
	Duplicates int `json:"duplicates"`
	Late       int `json:"late"`
	Invalid    int `json:"invalid"`
}
type Window struct {
	Store  string    `json:"store"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Count  int       `json:"count"`
	Amount int64     `json:"amount_minor"`
	Status string    `json:"status"`
}
type Step struct {
	Line      int        `json:"line"`
	Outcome   string     `json:"outcome"`
	Watermark *time.Time `json:"watermark"`
}
type Rejection struct {
	Line int    `json:"line"`
	Code string `json:"code"`
}
type Report struct {
	SchemaVersion int         `json:"schema_version"`
	WindowSeconds int64       `json:"window_seconds"`
	LagSeconds    int64       `json:"lag_seconds"`
	Summary       Summary     `json:"summary"`
	Watermark     *time.Time  `json:"watermark"`
	Windows       []Window    `json:"windows"`
	Trace         []Step      `json:"trace"`
	Rejections    []Rejection `json:"rejections"`
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
var storePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_-]{0,15}$`)
var utcPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)

// Я разбираю ключи токенами: обычный Unmarshal молча принимает повторный ключ.
// Я не возвращаю исходную строку в отчёт: для разбора ошибки достаточно номера.
func parseEvent(line []byte) (Event, string) {
	d := json.NewDecoder(bytes.NewReader(line))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return Event{}, "invalid_json"
	}
	fields := make(map[string]json.RawMessage, 4)
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return Event{}, "invalid_json"
		}
		name, ok := key.(string)
		if !ok {
			return Event{}, "invalid_json"
		}
		if _, exists := fields[name]; exists {
			return Event{}, "duplicate_key"
		}
		switch name {
		case "event_id", "store", "occurred_at", "amount_minor":
		default:
			return Event{}, "unknown_field"
		}
		var raw json.RawMessage
		if d.Decode(&raw) != nil {
			return Event{}, "invalid_json"
		}
		fields[name] = raw
	}
	if end, err := d.Token(); err != nil || end != json.Delim('}') {
		return Event{}, "invalid_json"
	}
	if _, err := d.Token(); err != io.EOF {
		return Event{}, "trailing_json"
	}
	if len(fields) != 4 {
		return Event{}, "missing_field"
	}
	for _, raw := range fields {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return Event{}, "null_field"
		}
	}
	var e Event
	var timestamp string
	if json.Unmarshal(fields["event_id"], &e.ID) != nil || json.Unmarshal(fields["store"], &e.Store) != nil || json.Unmarshal(fields["occurred_at"], &timestamp) != nil || json.Unmarshal(fields["amount_minor"], &e.Amount) != nil {
		return Event{}, "invalid_type"
	}
	if !idPattern.MatchString(e.ID) || !storePattern.MatchString(e.Store) || e.Amount < 1 || e.Amount > 1000000000000 {
		return Event{}, "invalid_value"
	}
	if !utcPattern.MatchString(timestamp) {
		return Event{}, "invalid_time"
	}
	e.At, err = time.Parse(time.RFC3339, timestamp)
	if err != nil || e.At.Year() < 2020 || e.At.Year() > 2099 {
		return Event{}, "invalid_time"
	}
	return e, ""
}

type key struct {
	start time.Time
	store string
}

// Run не пишет файлы. Только после успешного EOF я разрешаю CLI сохранить отчёт.
// Порядок строк — порядок прибытия; сортировка входа изменила бы смысл lateness.
func Run(r io.Reader, c Config) (Report, error) {
	if err := c.Validate(); err != nil {
		return Report{}, err
	}
	out := Report{SchemaVersion: 1, WindowSeconds: int64(c.Window / time.Second), LagSeconds: int64(c.Lag / time.Second), Windows: []Window{}, Trace: []Step{}, Rejections: []Rejection{}}
	seen := make(map[string]Event)
	groups := make(map[key]*Window)
	var maxTime time.Time
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 65536)
	for scanner.Scan() {
		out.Summary.Rows++
		line := out.Summary.Rows
		if line > c.MaxRows {
			return Report{}, fmt.Errorf("я остановил обработку: max-rows=%d", c.MaxRows)
		}
		e, code := parseEvent(scanner.Bytes())
		outcome := "invalid"
		if code != "" {
			out.Summary.Invalid++
			out.Rejections = append(out.Rejections, Rejection{line, code})
		} else if old, ok := seen[e.ID]; ok {
			// Один ID с другим содержимым — конфликт идентичности, а не дубль.
			if old != e {
				return Report{}, fmt.Errorf("я обнаружил конфликт event_id в строке %d", line)
			}
			out.Summary.Duplicates++
			outcome = "duplicate"
		} else {
			seen[e.ID] = e
			start := e.At.Truncate(c.Window)
			end := start.Add(c.Window)
			// Я сравниваю с ПРЕДЫДУЩИМ watermark. Граница end == watermark закрыта.
			if out.Watermark != nil && !end.After(*out.Watermark) {
				out.Summary.Late++
				outcome = "late"
				out.Rejections = append(out.Rejections, Rejection{line, "window_closed"})
			} else {
				k := key{start, e.Store}
				w, ok := groups[k]
				if !ok {
					if len(groups) >= c.MaxGroups {
						return Report{}, fmt.Errorf("я остановил обработку: max-groups=%d", c.MaxGroups)
					}
					w = &Window{Store: e.Store, Start: start, End: end, Status: "eof_preview"}
					groups[k] = w
				}
				w.Count++
				// max-rows и верхняя граница суммы на событие защищают int64 от overflow.
				w.Amount += e.Amount
				out.Summary.Accepted++
				outcome = "accepted"
				if e.At.After(maxTime) {
					maxTime = e.At
					watermark := maxTime.Add(-c.Lag)
					out.Watermark = &watermark
				}
			}
		}
		// Я сохраняю неизменяемое значение времени на каждом шаге для учебной трассы.
		out.Trace = append(out.Trace, Step{line, outcome, out.Watermark})
	}
	if scanner.Err() != nil {
		return Report{}, errors.New("я не прочитал весь вход: ошибка чтения или строка больше буфера 64 KiB")
	}
	if out.Summary.Accepted == 0 {
		return Report{}, errors.New("я не нашёл ни одного принятого события")
	}
	for _, w := range groups {
		// Watermark монотонен: для итогового статуса достаточно его последнего значения.
		if out.Watermark != nil && !w.End.After(*out.Watermark) {
			w.Status = "watermarked"
		}
		out.Windows = append(out.Windows, *w)
	}
	sort.Slice(out.Windows, func(i, j int) bool {
		a, b := out.Windows[i], out.Windows[j]
		if a.Start.Equal(b.Start) {
			return a.Store < b.Store
		}
		return a.Start.Before(b.Start)
	})
	return out, nil
}
