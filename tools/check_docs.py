#!/usr/bin/env python3
"""Check current documentation requirements, English prose, and local links.

The immutable 75-ID inventory lives in docs_requirements.json. Definitions
belong in product.md, and acceptance maps each definition exactly once.
Only the Python standard library is required; no Git history is needed.
"""
from __future__ import annotations

import argparse
import json
import re
import sys
import unicodedata
from collections import Counter
from pathlib import Path
from urllib.parse import unquote, urlsplit

CY_ID_RE = re.compile(r"\bCY-\d{2}-(?:\d{2}|[A-E])\b")
NUMBERED_HEADING_RE = re.compile(r"^#{1,6}\s+(.*)$")
MARKDOWN_LINK_RE = re.compile(r"!?\[[^\]]*\]\((<[^>]*>|[^)]*)\)")
REFERENCE_LINK_RE = re.compile(r"!?\[([^\]]+)\](?:\[([^\]]*)\])")
REFERENCE_DEFINITION_RE = re.compile(r"^\s{0,3}\[([^\]]+)\]:\s*(<[^>]+>|\S+)", re.M)
HTML_ID_RE = re.compile(r"\bid\s*=\s*[\"']([^\"']+)[\"']", re.I)
FENCE_RE = re.compile(r"^\s{0,3}(```+|~~~+)")


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
        if not any(part in {".git", ".venv", "node_modules", ".build", "__pycache__"} for part in path.parts)
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


def requirement_check(root: Path) -> list[str]:
    errors = []
    inventory = Path(__file__).with_name("docs_requirements.json")
    baseline = json.loads(inventory.read_text(encoding="utf-8"))["ids"]
    expected = set(baseline)
    if len(baseline) != 75 or len(expected) != 75:
        errors.append("stable-ID baseline must contain 75 unique IDs")
    product_path = root / "docs/product.md"
    matrix_path = root / "docs/acceptance/requirements.md"
    for path in (product_path, matrix_path):
        if not path.is_file():
            errors.append(f"missing required document: {path.relative_to(root)}")
    if errors:
        return errors
    product = product_path.read_text(encoding="utf-8")
    matrix = matrix_path.read_text(encoding="utf-8")
    definitions = Counter(re.findall(r'<a id="(cy-\d{2}-(?:\d{2}|[a-e]))"></a>', product))
    expected_anchors = {value.lower() for value in expected}
    for anchor in sorted(expected_anchors):
        count = definitions[anchor]
        if count != 1:
            errors.append(f"requirement definition {anchor.upper()} occurs {count} times; expected once")
    for anchor in sorted(set(definitions) - expected_anchors):
        errors.append(f"unexpected requirement definition: {anchor}")
    # Scope is explicit immediately after each definition anchor.
    for match in re.finditer(r'<a id="(cy-[^\"]+)"></a>(.*?)(?=<a id=|\Z)', product, re.S):
        anchor, section = match.groups()
        if not re.match(r'\s*\*\*Disposition: (Required|Optional|Excluded)\.\*\*', section):
            errors.append(f"requirement definition lacks disposition: {anchor}")
        section_labels = re.findall(r'^\*\*(CY-\d{2}-(?:\d{2}|[A-E]))\.\*\*', section, re.M)
        if section_labels != [anchor.upper()]:
            errors.append(f"requirement anchor {anchor} must identify exactly its matching definition")
    labels = Counter(re.findall(r'^\*\*(CY-\d{2}-(?:\d{2}|[A-E]))\.\*\*', product, re.M))
    for value in sorted(expected):
        if labels[value] != 1:
            errors.append(f"requirement ID {value} defined {labels[value]} times; expected once")
    declared = set(CY_ID_RE.findall(product))
    if declared != expected:
        errors.append("product stable IDs differ from fixed baseline: " + ", ".join(sorted(declared ^ expected)))
    mapping_links = re.findall(r'^\| \[(CY-\d{2}-(?:\d{2}|[A-E]))\]\(([^)]+)\)', matrix, re.M)
    mappings = Counter(value for value, _ in mapping_links)
    for value, destination in mapping_links:
        if destination != f"../product.md#{value.lower()}":
            errors.append(f"acceptance mapping {value} must link to its matching product definition")
    for value in sorted(expected):
        if mappings[value] != 1:
            errors.append(f"acceptance mapping {value} occurs {mappings[value]} times; expected once")
    for value in sorted(set(mappings) - expected):
        errors.append(f"unexpected acceptance mapping: {value}")
    chapters = re.findall(r'^## (\d+)\. ', product, re.M)
    if Counter(chapters) != Counter(map(str, range(1, 23))):
        errors.append("product must define chapters 1–22 exactly once")
    for scenario in "ABCDE":
        if not re.search(rf'^### Scenario {scenario}:', product, re.M):
            errors.append(f"missing scenario {scenario}")
    quality = ("Accuracy", "Continuity", "Persistence", "Reliability", "Extensibility", "Independence",
               "Interoperability", "Cross-device consistency", "Cross-platform support", "Controllability",
               "Security", "Observability", "Cost efficiency", "Ease of use", "Long-term evolution")
    for dimension in quality:
        if not re.search(rf'^\| {re.escape(dimension)} \|', product, re.M):
            errors.append(f"missing quality requirement: {dimension}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parent.parent)
    args = parser.parse_args()
    root = args.root.resolve()
    failures = requirement_check(root) + link_check(root) + cjk_check(root)
    for failure in failures:
        print(f"FAIL: {failure}")
    if failures:
        print(f"documentation checks failed: {len(failures)} issue(s)")
        return 1
    print("documentation checks passed (75 IDs, 22 chapters, A–E, 15 quality requirements, English, links)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
