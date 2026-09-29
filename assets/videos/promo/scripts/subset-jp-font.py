"""Build the Japanese fallback fonts from Noto Sans JP (OFL).

Usage: .venv/bin/python scripts/subset-jp-font.py <NotoSansJP[wght].ttf>

Writes assets/fonts/NotoSansJP-{Medium,SemiBold,Bold}.ttf: static instances of the variable font, subset to
the characters of every `ja` line in data/script.json, the `ja` table of data/strings.json, ASCII and common
Japanese punctuation. Re-run it after changing any Japanese text, otherwise new characters fall back to a
system font. Be Vietnam Pro has no kana or kanji, so the page lists these right after it.
"""
import json
import sys
from pathlib import Path

from fontTools import subset
from fontTools.ttLib import TTFont
from fontTools.varLib import instancer

root = Path(__file__).resolve().parent.parent
script = json.loads((root / "data/script.json").read_text())
strings = json.loads((root / "data/strings.json").read_text())

text = "".join(l.get("ja", "") for s in script["scenes"] for l in s["lines"])
text += "".join(strings["ja"].values())
text += "".join(chr(c) for c in range(0x20, 0x7F)) + "、。「」『』（）！？：・…ー〜"

for name, weight in (("Medium", 500), ("SemiBold", 600), ("Bold", 700)):
    font = instancer.instantiateVariableFont(TTFont(sys.argv[1]), {"wght": weight})
    opts = subset.Options()
    opts.layout_features = ["*"]
    sub = subset.Subsetter(opts)
    sub.populate(text=text)
    sub.subset(font)
    out = root / f"assets/fonts/NotoSansJP-{name}.ttf"
    font.save(out)
    print(f"{out.relative_to(root)}  {out.stat().st_size // 1024} KB")
