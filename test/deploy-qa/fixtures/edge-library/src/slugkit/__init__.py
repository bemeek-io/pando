"""Turn arbitrary strings into URL slugs."""

import re
import unicodedata

__all__ = ["slugify"]
__version__ = "0.3.1"


def slugify(text: str, sep: str = "-") -> str:
    normalized = unicodedata.normalize("NFKD", text).encode("ascii", "ignore").decode()
    words = re.findall(r"[a-zA-Z0-9]+", normalized.lower())
    return sep.join(words)
