#!/usr/bin/env python3
"""
Recursively compare dir_a/ and dir_b/ and output JSON with categorized differences.
"""

import os
import json
import sys
from pathlib import Path


def compare_directories(dir_a, dir_b):
    """Compare two directories recursively and return categorized file lists."""

    # Get sets of relative file paths for each directory
    files_a = set()
    files_b = set()

    # Walk through dir_a
    for root, dirs, filenames in os.walk(dir_a):
        for filename in filenames:
            # Get relative path from dir_a
            full_path = os.path.join(root, filename)
            rel_path = os.path.relpath(full_path, dir_a)
            files_a.add(rel_path)

    # Walk through dir_b
    for root, dirs, filenames in os.walk(dir_b):
        for filename in filenames:
            # Get relative path from dir_b
            full_path = os.path.join(root, filename)
            rel_path = os.path.relpath(full_path, dir_b)
            files_b.add(rel_path)

    # Initialize result categories
    only_in_a = []
    only_in_b = []
    changed = []
    same = []

    # Files only in dir_a
    for rel_path in sorted(files_a - files_b):
        only_in_a.append(rel_path)

    # Files only in dir_b
    for rel_path in sorted(files_b - files_a):
        only_in_b.append(rel_path)

    # Files in both directories
    for rel_path in sorted(files_a & files_b):
        path_a = os.path.join(dir_a, rel_path)
        path_b = os.path.join(dir_b, rel_path)

        # Compare file contents
        try:
            with open(path_a, "rb") as f1, open(path_b, "rb") as f2:
                content_a = f1.read()
                content_b = f2.read()

                if content_a == content_b:
                    same.append(rel_path)
                else:
                    changed.append(rel_path)
        except (IOError, OSError) as e:
            # If we can't read a file, treat it as changed
            print(f"Warning: Could not read file {rel_path}: {e}", file=sys.stderr)
            changed.append(rel_path)

    return {
        "only_in_a": only_in_a,
        "only_in_b": only_in_b,
        "changed": changed,
        "same": same,
    }


def main():
    """Main entry point."""
    # Default directories to compare
    dir_a = "dir_a"
    dir_b = "dir_b"

    # Compare directories
    result = compare_directories(dir_a, dir_b)

    # Output as single-line JSON
    print(json.dumps(result))


if __name__ == "__main__":
    main()
