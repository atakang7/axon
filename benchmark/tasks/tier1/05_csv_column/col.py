#!/usr/bin/env python3
import csv
import sys


def main():
    if len(sys.argv) != 3:
        print(f"Usage: {sys.argv[0]} <csv_file> <column_name>", file=sys.stderr)
        sys.exit(1)

    csv_file = sys.argv[1]
    column_name = sys.argv[2]

    try:
        with open(csv_file, "r", newline="") as f:
            reader = csv.DictReader(f)
            if column_name not in reader.fieldnames:
                print(f"Column '{column_name}' not found in CSV", file=sys.stderr)
                sys.exit(1)

            for row in reader:
                print(row[column_name])
    except FileNotFoundError:
        print(f"File '{csv_file}' not found", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"Error reading CSV: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
