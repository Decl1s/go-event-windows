package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/Decl1s/go-event-windows/internal/windowing"
	"io"
	"os"
)

const demo = `{"event_id":"e1","store":"NORTH","occurred_at":"2026-10-29T10:01:00Z","amount_minor":10000}
{"event_id":"e2","store":"NORTH","occurred_at":"2026-10-29T10:04:00Z","amount_minor":20000}
{"event_id":"e3","store":"SOUTH","occurred_at":"2026-10-29T10:02:00Z","amount_minor":7000}
{"event_id":"e1","store":"NORTH","occurred_at":"2026-10-29T10:01:00Z","amount_minor":10000}
{"event_id":"e4","store":"NORTH","occurred_at":"2026-10-29T10:08:00Z","amount_minor":30000}
{"event_id":"e5","store":"NORTH","occurred_at":"2026-10-29T10:03:00Z","amount_minor":9000}
{"event_id":"e6","store":"NORTH","occurred_at":"2026-10-29T10:06:00Z","amount_minor":5000}
{"event_id":"bad","store":"NORTH","occurred_at":"2099-01-01T00:00:00Z","amount_minor":-1}
{"event_id":"e7","store":"SOUTH","occurred_at":"2026-10-29T10:12:00Z","amount_minor":11000}
{"event_id":"e8","store":"NORTH","occurred_at":"2026-10-29T10:09:00Z","amount_minor":6000}
{"event_id":"e5","store":"NORTH","occurred_at":"2026-10-29T10:03:00Z","amount_minor":9000}
`

// Я не заменяю существующий файл. Отчёт заранее целиком подготовлен в памяти;
// при ошибке записи я убираю только созданный мной неполный файл.
func writeNew(path string, data []byte) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("я не создал выход: путь уже занят или недоступен")
	}
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Я ожидаю demo -out events.jsonl или run -in events.jsonl -out report.json [-window 5m -lag 2m]")
		return 2
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("out", "", "Новый выходной файл (я не перезаписываю старый)")
	var input *string
	c := windowing.DefaultConfig()
	if args[0] == "run" {
		input = fs.String("in", "", "JSONL: порядок строк означает порядок прибытия")
		fs.DurationVar(&c.Window, "window", c.Window, "Ширина непересекающегося UTC окна")
		fs.DurationVar(&c.Lag, "lag", c.Lag, "Отставание watermark от максимального принятого event time")
		fs.IntVar(&c.MaxRows, "max-rows", c.MaxRows, "Лимит всех строк")
		fs.IntVar(&c.MaxGroups, "max-groups", c.MaxGroups, "Лимит всех пар (окно, магазин)")
	} else if args[0] != "demo" {
		fmt.Fprintln(stderr, "Я поддерживаю только demo и run")
		return 2
	}
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || *output == "" || (input != nil && *input == "") {
		fmt.Fprintln(stderr, "Я ожидаю обязательные пути без позиционных аргументов")
		return 2
	}
	var data []byte
	if input == nil {
		data = []byte(demo)
	} else {
		if err := c.Validate(); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		f, err := os.Open(*input)
		if err != nil {
			fmt.Fprintln(stderr, "Я не открыл входной файл")
			return 1
		}
		result, err := windowing.Run(f, c)
		closeErr := f.Close()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if closeErr != nil {
			fmt.Fprintln(stderr, "Я не закрыл входной файл")
			return 1
		}
		data, err = json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, "Я не сформировал отчёт")
			return 1
		}
		data = append(data, '\n')
	}
	if err := writeNew(*output, data); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "Я сохранил новый файл. Пояснения и ожидаемые числа: README.md")
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
