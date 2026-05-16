# Task: extract emails

Write `emails.py` that reads stdin and prints every email-like token, one per line, deduplicated, sorted.

## Success
- Create `text.txt` containing 3 distinct emails embedded in prose, with one repeated.
- `python3 emails.py < text.txt` prints exactly the 3 unique emails, sorted.
