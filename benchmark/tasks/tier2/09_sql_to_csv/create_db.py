#!/usr/bin/env python3
import sqlite3
import sys


def main():
    # Read the SQL file
    with open("people.db.sql", "r") as f:
        sql = f.read()

    # Connect to database (creates file if not exists)
    conn = sqlite3.connect("people.db")
    cursor = conn.cursor()

    # Execute the SQL statements
    cursor.executescript(sql)
    conn.commit()
    conn.close()
    print("Database 'people.db' created successfully.")


if __name__ == "__main__":
    main()
