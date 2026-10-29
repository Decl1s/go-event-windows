"""Я проверяю явный список и собираю повторяемый ZIP только из исходников."""
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def source_files():
    names = (ROOT / "PUBLIC_FILES.txt").read_text(encoding="utf-8").splitlines()
    if not names or names != sorted(set(names)):
        raise ValueError("Я ожидаю отсортированный уникальный allowlist")
    for name in names:
        path = PurePosixPath(name)
        if path.is_absolute() or ".." in path.parts or "\\" in name:
            raise ValueError("Я запретил небезопасный путь")
        if any((ROOT.joinpath(*path.parts[:i])).is_symlink() for i in range(1, len(path.parts) + 1)):
            raise ValueError("Я не включаю symlink")
        if not ROOT.joinpath(name).is_file():
            raise ValueError("Я не нашёл разрешённый исходник")
    return names


def check_public(names):
    # Я ищу контакты и распространённые секреты; это помощь, а не DLP-гарантия.
    patterns = [r"[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}", r"gh[pousr]_" + r"[A-Za-z0-9]{20,}",
                r"github_pat_" + r"[A-Za-z0-9_]{20,}", r"-----BEGIN " + r"(?:RSA |OPENSSH )?PRIVATE KEY-----",
                r"/" + r"(?:home|Users|tmp)/[A-Za-z0-9]", r"[A-Z]:\\" + r"Users\\"]
    for name in names:
        text = ROOT.joinpath(name).read_bytes().decode("utf-8")
        if "\r" in text or any(re.search(pattern, text) for pattern in patterns):
            raise ValueError(f"Я остановил публикацию: проверка текста {name}")
    git_root = subprocess.check_output(["git", "rev-parse", "--show-toplevel"], cwd=ROOT, text=True).strip()
    if Path(git_root).resolve() != ROOT:
        raise ValueError("Я запретил родительский Git root")
    tracked = subprocess.check_output(["git", "ls-files", "-z"], cwd=ROOT).decode().split("\0")
    if sorted(x for x in tracked if x) != names:
        raise ValueError("Я ожидаю полное совпадение Git tree и allowlist")


def package():
    names = source_files()
    check_public(names)
    version = (ROOT / "VERSION").read_text().strip()
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        raise ValueError("Я ожидаю SemVer")
    prefix = f"go-event-windows-{version}"
    output = ROOT / "dist"
    output.mkdir(exist_ok=True)
    archive = output / f"{prefix}-source.zip"
    # Я выбираю STORE: байты ZIP одинаковы без зависимости от версии zlib.
    with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_STORED) as z:
        for name in names:
            info = zipfile.ZipInfo(f"{prefix}/{name}", (2020, 1, 1, 0, 0, 0))
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            z.writestr(info, ROOT.joinpath(name).read_bytes())
    with zipfile.ZipFile(archive) as z:
        if z.testzip() is not None or z.namelist() != [f"{prefix}/{n}" for n in names]:
            raise ValueError("Я не подтвердил дерево ZIP")
        for name in names:
            if z.read(f"{prefix}/{name}") != ROOT.joinpath(name).read_bytes():
                raise ValueError("Я обнаружил несовпадение исходника")
    sha = hashlib.sha256(archive.read_bytes()).hexdigest()
    (output / "SHA256SUMS").write_text(f"{sha}  {archive.name}\n", encoding="ascii", newline="\n")
    return {"archive": archive.name, "sha256": sha, "files": len(names), "bytes": archive.stat().st_size}


if __name__ == "__main__":
    print(json.dumps(package(), ensure_ascii=False))
