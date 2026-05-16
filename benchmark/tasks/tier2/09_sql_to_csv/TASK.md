# Task: SQL → CSV

Use `people.db.sql` to populate a sqlite database, then write `dump.py` that exports a CSV named `people.csv` containing all rows from the `people` table, sorted by age ascending. Header row required.

## Success
- `people.db` (sqlite file) exists in cwd.
- `python3 dump.py` produces `people.csv`.
- First line: `id,name,age,city`
- Body lines sorted by age ascending; for the seed data, expected order: eve, bob, alice, dave, carol.
