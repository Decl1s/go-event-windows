"""Я запускаю одинаковый учебный сценарий на Linux и Windows без постоянных служб."""
import argparse
import json
from pathlib import Path
import subprocess
import tempfile
from package_release import ROOT, package


def command(*args, success=True):
    result = subprocess.run(args, cwd=ROOT, text=True, encoding="utf-8", stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if (result.returncode == 0) != success:
        raise RuntimeError(f"Я получил неожиданный код {result.returncode}: {result.stdout}\n{result.stderr}")
    return result.stdout.strip()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--race", action="store_true")
    args = parser.parse_args()
    version = command("go", "version")
    if "go1.27.3 " not in version:
        raise RuntimeError("Я закрепил Go 1.27.3")
    if command("go", "list", "-m", "all") != "github.com/Decl1s/go-event-windows":
        raise RuntimeError("Я не ожидаю внешних Go-модулей")
    go_files = [str(p.relative_to(ROOT)) for p in ROOT.rglob("*.go") if ".cache" not in p.parts]
    if command("gofmt", "-l", *go_files):
        raise RuntimeError("Я ожидаю форматирование gofmt")
    command("go", "vet", "./...")
    test_flags = ["-race"] if args.race else []
    tests = command("go", "test", *test_flags, "-cover", "-count=1", "./...")
    with tempfile.TemporaryDirectory(prefix="event-windows-") as temporary:
        base = Path(temporary)
        binary = base / ("windowlab.exe" if __import__("os").name == "nt" else "windowlab")
        command("go", "build", "-trimpath", "-o", str(binary), "./cmd/windowlab")
        source, report, repeat, relaxed = [base / n for n in ("events.jsonl", "report.json", "repeat.json", "relaxed.json")]
        command(str(binary), "demo", "-out", str(source))
        command(str(binary), "run", "-in", str(source), "-out", str(report))
        data = json.loads(report.read_text(encoding="utf-8"))
        if data["summary"] != dict(rows=11, accepted=6, duplicates=2, late=2, invalid=1):
            raise RuntimeError("Я не подтвердил контрольные счётчики")
        if [w["amount_minor"] for w in data["windows"]] != [30000, 7000, 35000, 11000]:
            raise RuntimeError("Я не подтвердил суммы окон")
        if [w["status"] for w in data["windows"]] != ["watermarked"] * 3 + ["eof_preview"]:
            raise RuntimeError("Я не подтвердил незакрытый хвост")
        original = report.read_bytes()
        command(str(binary), "run", "-in", str(source), "-out", str(repeat))
        if original != repeat.read_bytes():
            raise RuntimeError("Я обнаружил неповторяемый результат")
        command(str(binary), "run", "-in", str(source), "-out", str(report), success=False)
        if original != report.read_bytes():
            raise RuntimeError("Я заменил существующий отчёт")
        command(str(binary), "run", "-in", str(source), "-out", str(relaxed), "-lag", "10m")
        relaxed_data = json.loads(relaxed.read_text())
        if relaxed_data["summary"]["accepted"] != 8 or relaxed_data["summary"]["late"] != 0 or sum(w["amount_minor"] for w in relaxed_data["windows"]) != 98000:
            raise RuntimeError("Я не подтвердил влияние lag")
        forbidden = base / "partial.json"
        command(str(binary), "run", "-in", str(source), "-out", str(forbidden), "-max-rows", "2", success=False)
        if forbidden.exists():
            raise RuntimeError("Я оставил частичный отчёт")
    first = package()
    if package() != first:
        raise RuntimeError("Я получил разный ZIP из одинаковых исходников")
    print(json.dumps({"go": version, "tests": tests, "demo": data["summary"], "relaxed_accepted": 8, "replay_identical": True, "protected_output": True, "archive": first}, ensure_ascii=False))


if __name__ == "__main__":
    main()
