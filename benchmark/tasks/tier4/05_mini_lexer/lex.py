def tokenize(src: str) -> list[tuple[str, str]]:
    """Tokenize the DSL source code."""
    tokens = []
    i = 0
    n = len(src)
    
    while i < n:
        ch = src[i]
        
        # Skip whitespace
        if ch in ' \t\n\r':
            i += 1
            continue
        
        # Line comment
        if ch == '/' and i + 1 < n and src[i + 1] == '/':
            i += 2
            while i < n and src[i] != '\n':
                i += 1
            # newline not consumed here, will be skipped as whitespace next iteration
            continue
        
        # Operators
        if ch in '+-*/=()':
            tokens.append(('OP', ch))
            i += 1
            continue
        
        # Numbers (integer or decimal)
        if ch.isdigit() or (ch == '.' and i + 1 < n and src[i + 1].isdigit()):
            start = i
            if ch == '.':
                i += 1
            while i < n and src[i].isdigit():
                i += 1
            if i < n and src[i] == '.':
                i += 1
                while i < n and src[i].isdigit():
                    i += 1
            # Ensure at least one digit
            if src[start:i].replace('.', '').isdigit():
                tokens.append(('NUMBER', src[start:i]))
                continue
            else:
                # This should not happen given the checks
                i = start  # fallback
        
        # Identifiers and keywords
        if ch.isalpha() or ch == '_':
            start = i
            while i < n and (src[i].isalnum() or src[i] == '_'):
                i += 1
            ident = src[start:i]
            if ident in ('let', 'print'):
                tokens.append(('KEYWORD', ident))
            else:
                tokens.append(('IDENT', ident))
            continue
        
        # String literals
        if ch == '"':
            start = i
            i += 1
            while i < n:
                if src[i] == '\\' and i + 1 < n:
                    # Escape sequence: skip two characters
                    i += 2
                elif src[i] == '"':
                    # Closing quote
                    i += 1
                    break
                else:
                    i += 1
            else:
                # Reached end of input without closing quote
                raise ValueError(f"unclosed string at pos {start}")
            tokens.append(('STRING', src[start:i]))
            continue
        
        # Unknown character
        raise ValueError(f"unexpected char {ch!r} at pos {i}")
    
    return tokens