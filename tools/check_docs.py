#!/usr/bin/env python3
"""Check the documentation archive and its English planning layer.

The archive mapping intentionally follows the original repository layout:
the PRD and three historical plans go to docs/archive/research/, numbered
reports go to docs/archive/research/reports/, and evidence keeps its relative
path below docs/archive/research/evidence/. The old README is accounted for by
the archive inventory because the current root README replaces it.

This script uses only the Python standard library and git. It is intended to
run from any directory inside the checkout.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
import unicodedata
from collections import Counter
from pathlib import Path, PurePosixPath
from urllib.parse import unquote, urlsplit


SOURCE_COMMIT = "f66e455"
SOURCE_ROOT_DOCS = (
    "Cylo-完整产品需求说明书.md",
    "Cylo-实现调研与架构方案.md",
    "Cylo-开源项目选型比较.md",
    "README.md",
    "codex-super-agent-requirements-and-architecture.md",
)
SOURCE_RESEARCH_DIR = "docs/research/"
EXPECTED_SOURCE_COUNT = 38
CY_ID_RE = re.compile(r"\bCY-\d{2}-(?:\d{2}|[A-E])\b")
NUMBERED_HEADING_RE = re.compile(r"^#{1,6}\s+(.*)$")
SECTION_NUMBER_RE = re.compile(r"(?<!\d)(\d+(?:\.\d+)+)(?!\d)")
CHAPTER_NUMBER_RE = re.compile(r"(?:^|\s)(?:第\s*)?(\d{1,2})(?:\s*章|[.、])")
MARKDOWN_LINK_RE = re.compile(r"!?\[[^\]]*\]\((<[^>]*>|[^)]*)\)")
REFERENCE_LINK_RE = re.compile(r"!?\[([^\]]+)\](?:\[([^\]]*)\])")
REFERENCE_DEFINITION_RE = re.compile(r"^\s{0,3}\[([^\]]+)\]:\s*(<[^>]+>|\S+)", re.M)
HTML_ID_RE = re.compile(r"\bid\s*=\s*[\"']([^\"']+)[\"']", re.I)
FENCE_RE = re.compile(r"^\s{0,3}(```+|~~~+)")


class CheckFailure(Exception):
    pass


def git(root: Path, *args: str, text: bool = True) -> str | bytes:
    result = subprocess.run(
        ["git", *args], cwd=root, check=False, stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if result.returncode:
        detail = result.stderr.decode("utf-8", "replace").strip()
        raise CheckFailure(f"git {' '.join(args)} failed: {detail}")
    return result.stdout.decode("utf-8", "replace") if text else result.stdout


def source_files(root: Path, commit: str) -> list[str]:
    raw = git(root, "-c", "core.quotePath=false", "ls-tree", "-r", "--name-only", commit)
    assert isinstance(raw, str)
    files = []
    for name in raw.splitlines():
        if name in SOURCE_ROOT_DOCS or name.startswith(SOURCE_RESEARCH_DIR):
            files.append(name)
    return sorted(files)


ROOT_ARCHIVE_NAMES = {
    "Cylo-完整产品需求说明书.md": "product-requirements.md",
    "Cylo-实现调研与架构方案.md": "superseded-implementation-plan.md",
    "Cylo-开源项目选型比较.md": "project-selection-comparison.md",
    "codex-super-agent-requirements-and-architecture.md": "codex-super-agent-requirements-and-architecture.md",
}


def expected_archive_path(source: str, archive_root: str) -> str | None:
    if source == "README.md":
        # The new root README replaces this former index; docs/archive/README.md
        # records the original path and points to the source commit in Git.
        return None
    if source.startswith("docs/research/evidence/"):
        return source.replace("docs/research/evidence/", f"{archive_root}/evidence/", 1)
    if source.startswith(SOURCE_RESEARCH_DIR):
        return source.replace("docs/research/", f"{archive_root}/reports/", 1)
    name = ROOT_ARCHIVE_NAMES.get(source)
    if name is None:
        raise CheckFailure(f"no archive mapping configured for source file: {source}")
    return f"{archive_root}/{name}"


def check_archive(root: Path, commit: str, archive_root: str) -> tuple[list[str], list[str]]:
    errors: list[str] = []
    files = source_files(root, commit)
    if len(files) != EXPECTED_SOURCE_COUNT:
        errors.append(
            f"{commit} source inventory has {len(files)} files; expected {EXPECTED_SOURCE_COUNT}"
        )
    inventory_path = root / "docs/archive/README.md"
    inventory = inventory_path.read_text(encoding="utf-8") if inventory_path.is_file() else ""
    if not inventory:
        errors.append("missing archive inventory: docs/archive/README.md")
    inventory_paths = set(re.findall(r"^\| `([^`]+)` \|", inventory, re.M))
    if inventory_paths != set(files):
        errors.append(
            "archive inventory path set differs: missing "
            + ", ".join(sorted(set(files) - inventory_paths))
            + "; unexpected "
            + ", ".join(sorted(inventory_paths - set(files)))
        )
    non_markdown_evidence = [
        path for path in files
        if path.startswith("docs/research/evidence/")
        and PurePosixPath(path).suffix.lower() != ".md"
    ]
    if len(non_markdown_evidence) != 11:
        errors.append(
            f"source inventory has {len(non_markdown_evidence)} non-Markdown evidence files; expected 11"
        )
    for source in files:
        if f"`{source}`" not in inventory:
            errors.append(f"archive inventory omits original source path: {source}")
        destination = expected_archive_path(source, archive_root)
        if destination is None:
            continue
        original = git(root, "show", f"{commit}:{source}", text=False)
        target = root / destination
        if not target.is_file():
            errors.append(f"missing archive copy: {destination} (from {source})")
            continue
        is_non_markdown_evidence = (
            source.startswith("docs/research/evidence/")
            and PurePosixPath(source).suffix.lower() != ".md"
        )
        if is_non_markdown_evidence and target.read_bytes() != original:
            errors.append(f"archive bytes differ: {destination} (from {source})")
    if "README.md (replaced)" not in inventory:
        errors.append("archive inventory must document the original README.md as replaced")
    return files, errors


def strip_markdown_for_scan(text: str, *, quoted: bool = False) -> str:
    output: list[str] = []
    fence_char: str | None = None
    fence_len = 0
    for line in text.splitlines():
        fence = FENCE_RE.match(line)
        if fence:
            marker = fence.group(1)
            char, length = marker[0], len(marker)
            if fence_char is None:
                fence_char, fence_len = char, length
            elif char == fence_char and length >= fence_len:
                fence_char, fence_len = None, 0
            continue
        if fence_char:
            continue
        if quoted and re.match(r"^\s*>\s?", line):
            continue
        # Inline code and HTML comments commonly hold source literals or paths.
        line = re.sub(r"`+[^`]*`+", "", line)
        line = re.sub(r"\]\([^)]*\)", ")", line)
        line = re.sub(r"[\w./-]*[\u3400-\u9fff][\w./-]*\.md\b", "", line)
        line = re.sub(r"<!--.*?-->", "", line)
        output.append(line)
    return "\n".join(output)


def has_cjk(text: str) -> bool:
    for char in text:
        code = ord(char)
        if (
            0x3400 <= code <= 0x4DBF
            or 0x4E00 <= code <= 0x9FFF
            or 0xF900 <= code <= 0xFAFF
            or 0x3040 <= code <= 0x30FF
            or 0xAC00 <= code <= 0xD7AF
        ):
            return True
    return False


def markdown_files(root: Path) -> list[Path]:
    return sorted(
        path for path in root.rglob("*.md")
        if ".git" not in path.parts
    )


def cjk_check(root: Path) -> list[str]:
    errors = []
    for path in markdown_files(root):
        cleaned = strip_markdown_for_scan(path.read_text(encoding="utf-8"), quoted=True)
        if has_cjk(cleaned):
            errors.append(f"remaining CJK narrative text: {path.relative_to(root)}")
    return errors


def slugify(heading: str) -> str:
    # GitHub heading anchors remove markup and punctuation, preserve underscores
    # and hyphens, lowercase, then replace whitespace with hyphens.
    value = re.sub(r"!?\[([^\]]+)\]\([^)]*\)", r"\1", heading)
    value = re.sub(r"<[^>]+>", "", value)
    value = re.sub(r"`+([^`]+)`+", r"\1", value)
    value = value.lower().strip()
    value = "".join(
        char for char in value
        if char in "-_ " or char.isalnum() or unicodedata.category(char)[0] in "LM"
    )
    return re.sub(r"\s+", "-", value)


def heading_anchors(text: str) -> set[str]:
    anchors: set[str] = set()
    counts: Counter[str] = Counter()
    in_fence: tuple[str, int] | None = None
    for line in text.splitlines():
        fence = FENCE_RE.match(line)
        if fence:
            marker = fence.group(1)
            char, length = marker[0], len(marker)
            if in_fence is None:
                in_fence = (char, length)
            elif char == in_fence[0] and length >= in_fence[1]:
                in_fence = None
            continue
        if in_fence:
            continue
        heading = NUMBERED_HEADING_RE.match(line)
        if heading:
            title = re.sub(r"\s+#+\s*$", "", heading.group(1)).strip()
            base = slugify(title)
            suffix = counts[base]
            counts[base] += 1
            anchors.add(base if suffix == 0 else f"{base}-{suffix}")
        for html_id in HTML_ID_RE.findall(line):
            anchors.add(html_id)
    return anchors


def split_link_destination(value: str) -> str:
    value = value.strip()
    if value.startswith("<") and value.endswith(">"): 
        value = value[1:-1]
    # A title may follow a destination separated by whitespace.
    if value.startswith("<"):
        end = value.find(">")
        if end >= 0:
            return value[1:end]
    return value.split(None, 1)[0] if value else ""


def normalize_reference_id(value: str) -> str:
    return " ".join(value.split()).casefold()


def check_destination(
    root: Path,
    source: Path,
    destination: str,
    anchors_by_path: dict[Path, set[str]],
    errors: list[str],
) -> None:
    destination = split_link_destination(destination)
    if not destination:
        return
    decoded = unquote(destination)
    parsed = urlsplit(decoded)
    if parsed.scheme or parsed.netloc:
        return
    target_text = parsed.path
    fragment = parsed.fragment
    if target_text:
        target = (source.parent / target_text).resolve()
        if not target.exists():
            errors.append(
                f"broken local link in {source.relative_to(root)}: {destination}"
            )
            return
    else:
        target = source.resolve()
    if fragment and not target.is_dir() and target.suffix.lower() == ".md":
        anchors = anchors_by_path.get(target)
        if anchors is None:
            try:
                anchors = heading_anchors(target.read_text(encoding="utf-8"))
            except (OSError, UnicodeError):
                anchors = set()
        if fragment not in anchors:
            errors.append(
                f"broken Markdown heading link in {source.relative_to(root)}: {destination}"
            )


def link_check(root: Path) -> list[str]:
    errors: list[str] = []
    files = markdown_files(root)
    anchors_by_path = {path.resolve(): heading_anchors(path.read_text(encoding="utf-8")) for path in files}
    for source in files:
        text = source.read_text(encoding="utf-8")
        definitions = {
            normalize_reference_id(identifier): destination
            for identifier, destination in REFERENCE_DEFINITION_RE.findall(text)
        }
        for match in MARKDOWN_LINK_RE.finditer(text):
            check_destination(root, source, match.group(1), anchors_by_path, errors)
        for match in REFERENCE_LINK_RE.finditer(text):
            identifier = match.group(2) or match.group(1)
            destination = definitions.get(normalize_reference_id(identifier))
            if destination is None:
                errors.append(
                    f"unresolved Markdown link reference in {source.relative_to(root)}: [{identifier}]"
                )
            else:
                check_destination(root, source, destination, anchors_by_path, errors)
    return errors


def stable_id_check(root: Path, commit: str, master_path: Path) -> list[str]:
    source = git(root, "show", f"{commit}:docs/research/01-requirements.md")
    assert isinstance(source, str)
    source_ids = set(CY_ID_RE.findall(source))
    master = master_path.read_text(encoding="utf-8")
    master_ids = set(CY_ID_RE.findall(master))
    missing = sorted(source_ids - master_ids)
    errors = []
    if len(source_ids) != 75:
        errors.append(f"source stable-ID inventory has {len(source_ids)} IDs; expected 75")
    if missing:
        errors.append("master plan omits stable IDs: " + ", ".join(missing))
    return errors


def chapter_number(heading: str) -> int | None:
    match = CHAPTER_NUMBER_RE.search(heading)
    if match:
        return int(match.group(1))
    chinese = re.search(r"^([一二三四五六七八九十]+)[、章.\s]", heading)
    if chinese:
        values = {"一": 1, "二": 2, "三": 3, "四": 4, "五": 5, "六": 6, "七": 7, "八": 8, "九": 9, "十": 10}
        token = chinese.group(1)
        if token == "十":
            return 10
        if token.startswith("十"):
            return 10 + values.get(token[1:], 0)
        if token.endswith("十"):
            return values.get(token[0], 0) * 10
        if "十" in token:
            left, right = token.split("十", 1)
            return values.get(left, 1) * 10 + values.get(right, 0)
        return values.get(token)
    # English headings commonly use "1. Product" or "Chapter 1".
    match = re.search(r"(?:chapter\s+)?(\d{1,2})(?:[.)\s:–-])", heading, re.I)
    return int(match.group(1)) if match else None


def prd_structure(text: str) -> tuple[dict[int, int], set[int]]:
    counts: Counter[int] = Counter()
    chapters: set[int] = set()
    active_chapter: int | None = None
    in_fence: tuple[str, int] | None = None
    for line in text.splitlines():
        fence = FENCE_RE.match(line)
        if fence:
            marker = fence.group(1)
            char, length = marker[0], len(marker)
            if in_fence is None:
                in_fence = (char, length)
            elif char == in_fence[0] and length >= in_fence[1]:
                in_fence = None
            continue
        if in_fence:
            continue
        match = NUMBERED_HEADING_RE.match(line)
        if not match:
            continue
        title = match.group(1).strip()
        chapter = chapter_number(title)
        if len(line) - len(line.lstrip("#")) == 1 and chapter is not None:
            active_chapter = chapter
            chapters.add(chapter)
        elif active_chapter is not None:
            section = SECTION_NUMBER_RE.search(title)
            if section and int(section.group(1).split(".", 1)[0]) == active_chapter:
                counts[active_chapter] += 1
    return dict(counts), chapters


def prd_structure_check(root: Path, commit: str, translation: Path) -> list[str]:
    original = git(root, "show", f"{commit}:Cylo-完整产品需求说明书.md")
    assert isinstance(original, str)
    translated = translation.read_text(encoding="utf-8")
    original_counts, original_chapters = prd_structure(original)
    translated_counts, translated_chapters = prd_structure(translated)
    errors = []
    expected_chapters = set(range(1, 23))
    if original_chapters != expected_chapters:
        errors.append(
            "original PRD chapter parse did not find chapters 1–22: "
            + ", ".join(map(str, sorted(original_chapters)))
        )
    if translated_chapters != original_chapters:
        errors.append(
            "translated PRD chapter set differs: missing "
            + ", ".join(map(str, sorted(original_chapters - translated_chapters)))
            + "; extra "
            + ", ".join(map(str, sorted(translated_chapters - original_chapters)))
        )
    for chapter in sorted(original_chapters):
        if original_counts.get(chapter, 0) != translated_counts.get(chapter, 0):
            errors.append(
                f"translated PRD chapter {chapter} has {translated_counts.get(chapter, 0)} "
                f"numbered subsections; original has {original_counts.get(chapter, 0)}"
            )
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-commit", default=SOURCE_COMMIT)
    parser.add_argument("--archive-root", default="docs/archive/research")
    parser.add_argument("--master-plan", default="docs/master-plan.md")
    parser.add_argument(
        "--prd-translation", default="docs/archive/research/product-requirements.md"
    )
    parser.add_argument(
        "--skip-prd-structure", action="store_true",
        help="skip translated full PRD structure check (useful before translation lands)",
    )
    args = parser.parse_args()
    root_raw = git(Path.cwd(), "rev-parse", "--show-toplevel")
    assert isinstance(root_raw, str)
    root = Path(root_raw.strip())
    master = root / args.master_plan
    translation = root / args.prd_translation
    failures: list[str] = []

    files, archive_errors = check_archive(root, args.source_commit, args.archive_root)
    failures.extend(archive_errors)
    if not master.is_file():
        failures.append(f"missing master plan: {args.master_plan}")
    else:
        failures.extend(stable_id_check(root, args.source_commit, master))
    if not args.skip_prd_structure:
        if not translation.is_file():
            failures.append(f"missing translated PRD: {args.prd_translation}")
        else:
            failures.extend(prd_structure_check(root, args.source_commit, translation))
    failures.extend(link_check(root))
    failures.extend(cjk_check(root))

    print(f"source inventory: {len(files)} files from {args.source_commit}")
    if failures:
        for failure in failures:
            print(f"FAIL: {failure}")
        print(f"documentation checks failed: {len(failures)} issue(s)")
        return 1
    print("documentation checks passed")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except CheckFailure as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(2)
