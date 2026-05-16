def balanced(s: str) -> bool:
    """
    Return True iff the string s has correctly matched (), [], {} brackets.
    Other characters are ignored.
    """
    stack = []
    pairs = {")": "(", "]": "[", "}": "{"}

    for ch in s:
        if ch in "([{":
            stack.append(ch)
        elif ch in ")]}":
            if not stack or stack[-1] != pairs[ch]:
                return False
            stack.pop()
    return not stack
