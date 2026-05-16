#!/usr/bin/env python3
import sys


def count_words(filename):
    """Count whitespace-separated tokens in a file."""
    try:
        with open(filename, "r", encoding="utf-8") as f:
            text = f.read()
        # Split on any whitespace and filter out empty strings
        words = text.split()
        return len(words)
    except FileNotFoundError:
        print(f"Error: File '{filename}' not found.", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"Error reading file: {e}", file=sys.stderr)
        sys.exit(1)


def main():
    if len(sys.argv) != 2:
        print("Usage: python3 wc.py <filename>", file=sys.stderr)
        sys.exit(1)

    filename = sys.argv[1]
    word_count = count_words(filename)
    print(word_count)


if __name__ == "__main__":
    main()
