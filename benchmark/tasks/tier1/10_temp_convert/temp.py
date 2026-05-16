#!/usr/bin/env python3
import sys


def celsius_to_fahrenheit(celsius):
    """Convert Celsius to Fahrenheit."""
    return (celsius * 9 / 5) + 32


def fahrenheit_to_celsius(fahrenheit):
    """Convert Fahrenheit to Celsius."""
    return (fahrenheit - 32) * 5 / 9


def main():
    if len(sys.argv) != 3:
        print("Usage: python3 temp.py <number> <unit>")
        print("Example: python3 temp.py 100 C")
        sys.exit(1)

    try:
        value = float(sys.argv[1])
        unit = sys.argv[2].upper()
    except ValueError:
        print("Error: First argument must be a number")
        sys.exit(1)

    if unit == "C":
        result = celsius_to_fahrenheit(value)
    elif unit == "F":
        result = fahrenheit_to_celsius(value)
    else:
        print(f"Error: Unit must be 'C' or 'F', got '{sys.argv[2]}'")
        sys.exit(1)

    # Print result rounded to 2 decimal places
    print(f"{result:.2f}")


if __name__ == "__main__":
    main()
