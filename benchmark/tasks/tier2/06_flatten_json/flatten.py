#!/usr/bin/env python3
import json
import sys


def flatten(obj, parent_key=""):
    """Recursively flatten a JSON object, returning dict of dot-path keys to values."""
    items = {}
    if isinstance(obj, dict):
        for k, v in obj.items():
            new_key = f"{parent_key}.{k}" if parent_key else k
            items.update(flatten(v, new_key))
    elif isinstance(obj, list):
        for i, v in enumerate(obj):
            new_key = f"{parent_key}.{i}" if parent_key else str(i)
            items.update(flatten(v, new_key))
    else:
        # primitive value
        items[parent_key] = obj
    return items


def main():
    try:
        with open("input.json", "r") as f:
            data = json.load(f)
    except FileNotFoundError:
        print("Error: input.json not found", file=sys.stderr)
        sys.exit(1)
    except json.JSONDecodeError as e:
        print(f"Error: invalid JSON: {e}", file=sys.stderr)
        sys.exit(1)

    flat = flatten(data)
    # sort keys alphabetically
    for key in sorted(flat.keys()):
        value = flat[key]
        # Convert boolean to Python's str representation (True/False) per example
        if isinstance(value, bool):
            value = str(value)
        else:
            value = str(value)
        print(f"{key}={value}")


if __name__ == "__main__":
    main()
