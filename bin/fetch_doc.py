#!/usr/bin/env python3
import argparse
import datetime as dt
import hashlib
import json
import re
import urllib.request
from html.parser import HTMLParser
from pathlib import Path


class TextExtractor(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.parts: list[str] = []

    def handle_data(self, data: str) -> None:
        text = data.strip()
        if text:
            self.parts.append(text)

    def text(self) -> str:
        raw = "\n".join(self.parts)
        raw = re.sub(r"\n{3,}", "\n\n", raw)
        return raw.strip()


def main() -> int:
    parser = argparse.ArgumentParser(description="Fetch URL and store clean text source.")
    parser.add_argument("--url", required=True)
    parser.add_argument("--slug", required=True)
    parser.add_argument("--lang", default="unknown")
    parser.add_argument("--docs-dir", default="docs/sources")
    args = parser.parse_args()

    target = Path(args.docs_dir) / args.slug
    target.mkdir(parents=True, exist_ok=True)

    with urllib.request.urlopen(args.url) as resp:
        html = resp.read().decode("utf-8", errors="ignore")

    extractor = TextExtractor()
    extractor.feed(html)
    text = extractor.text()

    source_path = target / "source.md"
    source_path.write_text(text + "\n", encoding="utf-8")

    checksum = hashlib.sha256(source_path.read_bytes()).hexdigest()
    meta = {
        "url": args.url,
        "fetched_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "language": args.lang,
        "checksum_sha256": checksum,
    }
    (target / "meta.json").write_text(json.dumps(meta, indent=2) + "\n", encoding="utf-8")

    print(f"Saved: {source_path}")
    print(f"Saved: {target / 'meta.json'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
