#!/usr/bin/env python3
import sys
import re


def main():
    if len(sys.argv) != 3:
        sys.stderr.write("Usage: python3 mygrep.py <regex> <filename>\n")
        sys.exit(1)

    pattern = sys.argv[1]
    filename = sys.argv[2]

    try:
        regex = re.compile(pattern)
    except re.error as e:
        sys.stderr.write(f"Invalid regex: {e}\n")
        sys.exit(1)

    try:
        with open(filename, "r") as f:
            for line in f:
                # Remove trailing newline for matching, but keep original line for output
                if regex.search(line.rstrip("\n")):
                    # Print the line without adding extra newline (line already has it)
                    sys.stdout.write(line)
    except FileNotFoundError:
        sys.stderr.write(f"File not found: {filename}\n")
        sys.exit(1)
    except IOError as e:
        sys.stderr.write(f"IO error reading {filename}: {e}\n")
        sys.exit(1)


if __name__ == "__main__":
    main()
