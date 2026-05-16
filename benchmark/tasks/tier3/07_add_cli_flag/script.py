#!/usr/bin/env python3
"""Echoes lines from a file."""

import sys
import argparse


def main():
    parser = argparse.ArgumentParser(description="Echoes lines from a file.")
    parser.add_argument("file", help="input file")
    parser.add_argument("--upper", action="store_true", help="uppercase each line")
    args = parser.parse_args()

    with open(args.file) as f:
        for line in f:
            text = line.rstrip("\n")
            if args.upper:
                text = text.upper()
            print(text)


if __name__ == "__main__":
    main()
