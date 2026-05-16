#!/usr/bin/env python3
import json
import sys


def convert(val, indent=0, in_list=False):
    """Convert Python object to formatted string with 2-space indentation."""
    spaces = "  " * indent
    if isinstance(val, dict):
        lines = []
        for k, v in val.items():
            # Key line
            key_line = f"{spaces}{k}:"
            if isinstance(v, (dict, list)) or v is None:
                lines.append(key_line)
                lines.append(convert(v, indent + 1))
            else:
                lines.append(f"{key_line} {value_str(v, indent + 1)}")
        return "\n".join(lines)
    elif isinstance(val, list):
        lines = []
        for item in val:
            # List item dash at current indent
            dash = f"{spaces}-"
            if isinstance(item, (dict, list)):
                lines.append(dash)
                lines.append(convert(item, indent + 1, in_list=True))
            else:
                lines.append(f"{dash} {value_str(item, indent + 1)}")
        return "\n".join(lines)
    else:
        # Scalar value
        return f"{spaces}{value_str(val, indent)}"


def value_str(v, indent):
    """Format a scalar value according to formatting rules."""
    if isinstance(v, bool):
        return "true" if v else "false"
    elif v is None:
        return "null"
    elif isinstance(v, (int, float)):
        return str(v)
    else:  # string
        s = str(v)
        # Quote if contains ':' or starts with whitespace or looks like a number
        # to preserve string type (parses unquoted numbers as int/float)
        if ":" in s or s.lstrip() != s or s.isdigit():
            # Use single quotes, escaping single quotes by doubling
            return "'" + s.replace("'", "''") + "'"
        return s


def main():
    try:
        with open("data.json", "r") as f:
            data = json.load(f)
    except FileNotFoundError:
        sys.stderr.write("Error: data.json not found\n")
        sys.exit(1)
    except json.JSONDecodeError as e:
        sys.stderr.write(f"Error: invalid JSON: {e}\n")
        sys.exit(1)

    output = convert(data)
    print(output)


if __name__ == "__main__":
    main()
