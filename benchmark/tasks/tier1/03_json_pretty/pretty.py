#!/usr/bin/env python3
import json
import sys


def main():
    # Read JSON from stdin
    input_text = sys.stdin.read()

    # Parse JSON
    try:
        data = json.loads(input_text)
    except json.JSONDecodeError as e:
        print(f"Error: Invalid JSON input: {e}", file=sys.stderr)
        sys.exit(1)

    # Pretty-print with sorted keys and 2-space indent
    pretty_json = json.dumps(data, indent=2, sort_keys=True)

    # Write to stdout
    print(pretty_json)


if __name__ == "__main__":
    main()
