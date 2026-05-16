#!/usr/bin/env python3
import sqlite3
import csv


def main():
    conn = sqlite3.connect("people.db")
    cursor = conn.cursor()

    # Select all rows sorted by age ascending
    cursor.execute("SELECT id, name, age, city FROM people ORDER BY age ASC")
    rows = cursor.fetchall()

    # Write to CSV with header
    with open("people.csv", "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["id", "name", "age", "city"])
        writer.writerows(rows)

    conn.close()
    print("CSV 'people.csv' created successfully.")


if __name__ == "__main__":
    main()
