#!/usr/bin/env python3
"""
Calculate total apparent size in bytes of all files under a directory (recursive),
matching `du -sb` (i.e., st_size for regular files and directories).
"""

import os
import sys


def dir_size(path: str) -> int:
    """Return total apparent size in bytes of all files under path."""
    total = 0
    for dirpath, dirnames, filenames in os.walk(path):
        # Add size of the directory itself (st_size of the directory file)
        try:
            total += os.stat(dirpath).st_size
        except OSError:
            pass
        for filename in filenames:
            filepath = os.path.join(dirpath, filename)
            try:
                # Add size of regular file (st_size)
                total += os.stat(filepath).st_size
            except OSError:
                # ignore missing/unreadable files
                continue
    return total


def main() -> None:
    if len(sys.argv) != 2:
        print(f"Usage: {sys.argv[0]} <directory>", file=sys.stderr)
        sys.exit(1)
    path = sys.argv[1]
    if not os.path.isdir(path):
        print(f"Error: '{path}' is not a directory", file=sys.stderr)
        sys.exit(1)
    try:
        size = dir_size(path)
        print(size)
    except OSError as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
