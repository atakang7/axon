#!/usr/bin/env python3
import sys
from collections import defaultdict


def main():
    if len(sys.argv) > 1:
        log_path = sys.argv[1]
    else:
        log_path = "access.log"

    error_counts = defaultdict(int)

    try:
        with open(log_path, "r") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                # Parse level and user
                parts = line.split()
                if len(parts) < 3:
                    continue
                level = parts[2]
                if level != "ERROR":
                    continue
                # Find user=... token
                for token in parts[3:]:
                    if token.startswith("user="):
                        user = token[5:]  # strip 'user='
                        error_counts[user] += 1
                        break
    except FileNotFoundError:
        print(f"Error: file '{log_path}' not found", file=sys.stderr)
        sys.exit(1)

    # Sort: error count descending, then user ascending
    sorted_items = sorted(error_counts.items(), key=lambda x: (-x[1], x[0]))

    for user, count in sorted_items:
        print(f"{user}: {count}")


if __name__ == "__main__":
    main()
