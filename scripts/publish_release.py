"""Я публикую проверенный source ZIP через временный токен только в Actions main."""
import hashlib
import json
import os
import re
import subprocess
import urllib.error
import urllib.request
from package_release import ROOT, package

REPO = "Decl1s/go-event-windows"


def api(path, method="GET", payload=None, raw=None):
    base = "https://api.github.com/repos/" + REPO
    if path.startswith("/upload/"):
        base = "https://uploads.github.com/repos/" + REPO + "/releases/"
        path = path[len("/upload/"):]
    data = raw if raw is not None else (json.dumps(payload).encode() if payload is not None else None)
    request = urllib.request.Request(base + path, data=data, method=method, headers={
        "Authorization": "Bearer " + os.environ["GITHUB_TOKEN"],
        "Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28",
        "Content-Type": "application/octet-stream" if raw is not None else "application/json", "User-Agent": "go-event-windows-release"})
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return None
        raise RuntimeError(f"Я получил GitHub HTTP {error.code}") from None


def main():
    sha = os.environ.get("GITHUB_SHA", "")
    if (os.environ.get("GITHUB_REPOSITORY") != REPO or os.environ.get("GITHUB_EVENT_NAME") != "push" or os.environ.get("GITHUB_REF") != "refs/heads/main" or not re.fullmatch(r"[a-f0-9]{40}", sha)):
        raise RuntimeError("Я разрешаю выпуск только из push main этого репозитория")
    if subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip() != sha:
        raise RuntimeError("Я обнаружил несовпадение HEAD")
    subprocess.run(["git", "diff", "--exit-code", "HEAD", "--"], cwd=ROOT, check=True)
    evidence = package()
    version = (ROOT / "VERSION").read_text().strip()
    tag = "v" + version
    ref = api("/git/ref/tags/" + tag)
    if ref and (ref["object"]["type"] != "commit" or ref["object"]["sha"] != sha):
        raise RuntimeError("Я не перемещаю существующий тег")
    release = api("/releases/tags/" + tag)
    if release is None:
        drafts = [x for x in api("/releases?per_page=100") if x["tag_name"] == tag]
        if len(drafts) > 1:
            raise RuntimeError("Я обнаружил несколько черновиков")
        release = drafts[0] if drafts else api("/releases", "POST", {"tag_name": tag, "target_commitish": sha, "name": "Мои окна событий Go " + version, "draft": True, "prerelease": False, "body": "Я выпускаю учебный replay продаж: event-time окна, watermark, дубли, опоздания и EOF preview. Я отделяю завершённое окно от незакрытого хвоста. README ведёт по демонстрации; NOTES содержит разбор решений и пять заданий. ZIP включает только разрешённые исходники; результаты, окружение и контакты я не включаю."})
    if release["target_commitish"] != sha:
        raise RuntimeError("Я не перезаписываю чужой релиз")
    assets = api(f'/releases/{release["id"]}/assets')
    for name in [evidence["archive"], "SHA256SUMS"]:
        raw = (ROOT / "dist" / name).read_bytes()
        digest = "sha256:" + hashlib.sha256(raw).hexdigest()
        old = next((x for x in assets if x["name"] == name), None)
        item = old or api(f'/upload/{release["id"]}/assets?name={name}', "POST", raw=raw)
        if item["digest"] != digest:
            raise RuntimeError("Я обнаружил конфликт контрольной суммы")
    if release["draft"]:
        release = api(f'/releases/{release["id"]}', "PATCH", {"draft": False, "make_latest": "true"})
    final_ref = api("/git/ref/tags/" + tag)
    if release["draft"] or release["prerelease"] or final_ref["object"]["sha"] != sha:
        raise RuntimeError("Я не подтвердил публичный релиз")
    print(json.dumps({"release": release["html_url"], "commit": sha, **evidence}, ensure_ascii=False))


if __name__ == "__main__":
    main()
