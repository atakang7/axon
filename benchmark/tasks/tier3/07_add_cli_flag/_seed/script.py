#!/usr/bin/env python3
"""Echoes lines from a file."""
import sys

def main():
    if len(sys.argv) != 2:
        print("usage: script.py FILE", file=sys.stderr)
        sys.exit(2)
    path = sys.argv[1]
    with open(path) as f:
        for line in f:
            print(line.rstrip("\n"))

if __name__ == "__main__":
    main()
