#!/usr/bin/env python3
import sys


def main():
    seen = set()
    output_lines = []

    for line in sys.stdin:
        if line not in seen:
            seen.add(line)
            output_lines.append(line)

    sys.stdout.write("".join(output_lines))


if __name__ == "__main__":
    main()
