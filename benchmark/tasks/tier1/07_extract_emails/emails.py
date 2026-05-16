#!/usr/bin/env python3
"""
Extract email-like tokens from stdin, deduplicate, sort, print one per line.
"""

import sys
import re


def extract_emails(text):
    """
    Return list of email-like tokens found in text.
    Simple pattern: word chars, dots, hyphens, plus signs before @, domain with dots.
    """
    # RFC 5322-ish but simplified: local-part@domain
    # local-part: alnum and !#$%&'*+-/=?^_`{|}~. (allow dots, hyphens, plus)
    # domain: alnum and hyphens, dots for subdomains, at least one dot
    # This regex captures common email formats, may miss edge cases but good enough.
    pattern = r"\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b"
    return re.findall(pattern, text)


def main():
    text = sys.stdin.read()
    emails = extract_emails(text)
    # deduplicate while preserving order for consistent sorting
    unique = sorted(set(emails))
    for email in unique:
        print(email)


if __name__ == "__main__":
    main()
