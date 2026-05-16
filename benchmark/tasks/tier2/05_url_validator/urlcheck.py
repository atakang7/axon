import re


def is_valid(url):
    """Return True if url is a valid HTTP/HTTPS URL."""
    if not url:
        return False

    # Check for whitespace anywhere
    if re.search(r"\s", url):
        return False

    # Must start with http:// or https://
    if not (url.startswith("http://") or url.startswith("https://")):
        return False

    # Remove scheme
    if url.startswith("http://"):
        rest = url[7:]  # len('http://') = 7
    else:  # https://
        rest = url[8:]  # len('https://') = 8

    # Split host from optional path
    parts = rest.split("/", 1)
    host = parts[0]

    # Host must be non-empty, no spaces (already checked), and contain a dot
    if not host or "." not in host:
        return False

    # Host cannot contain spaces (already checked) and should be valid
    # Additional check: host shouldn't end with dot or have consecutive dots
    if host.startswith(".") or host.endswith(".") or ".." in host:
        return False

    # Optional path allowed - no further validation needed
    return True
