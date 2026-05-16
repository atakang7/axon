#!/usr/bin/env python3
import sys


def main():
    # Read all lines from stdin
    lines = sys.stdin.readlines()
    # Print lines in reverse order
    for line in reversed(lines):
        # Don't add extra newline if the line already has one
        sys.stdout.write(line)


if __name__ == "__main__":
    main()
