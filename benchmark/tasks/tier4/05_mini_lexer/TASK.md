# Task: tokenize a tiny DSL

Write `lex.py` exposing `tokenize(src: str) -> list[tuple[str, str]]` for this DSL:

- Tokens: `NUMBER` (integer or decimal), `IDENT` (letter then letters/digits/underscore), `OP` (one of `+ - * / = ( )`), `STRING` (double-quoted, no escapes needed beyond `\"` and `\\`), `KEYWORD` (`let`, `print`).
- Whitespace separates tokens but is not a token itself.
- `//` starts a line comment to end of line, not a token.
- Unknown char raises `ValueError(f"unexpected char {c!r} at pos {i}")`.

## Success
- `python3 -c "from lex import tokenize; print(tokenize('let x = 3.5 + \"hi\" // comment\\nprint(x)'))"` produces a list whose token-types in order are: `KEYWORD,IDENT,OP,NUMBER,OP,STRING,KEYWORD,OP,IDENT,OP`.
- The two `KEYWORD` tokens have values `let` and `print`.
- Lexing `"@"` raises `ValueError` with `"unexpected char"` in the message.
