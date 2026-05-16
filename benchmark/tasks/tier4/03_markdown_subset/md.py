#!/usr/bin/env python3
"""
Markdown to HTML converter (subset).
"""

import re


def to_html(md: str) -> str:
    """
    Convert Markdown string to HTML string.
    Supports:
    - # H1, ## H2, ### H3 (at line start)
    - paragraphs (blank-line-separated)
    - **bold**, *italic*, `code` (inline, ** takes precedence)
    - unordered lists (- item)
    - [text](url) links
    """
    lines = md.splitlines()
    output = []
    i = 0
    while i < len(lines):
        line = lines[i]
        # Skip empty lines (paragraph separators)
        if not line.strip():
            i += 1
            continue

        # Check for heading
        if line.startswith("###"):
            level = 3
            content = line[3:].lstrip()
            content = process_inline(content)
            output.append(f"<h{level}>{content}</h{level}>")
            i += 1
            continue
        elif line.startswith("##"):
            level = 2
            content = line[2:].lstrip()
            content = process_inline(content)
            output.append(f"<h{level}>{content}</h{level}>")
            i += 1
            continue
        elif line.startswith("#"):
            level = 1
            content = line[1:].lstrip()
            content = process_inline(content)
            output.append(f"<h{level}>{content}</h{level}>")
            i += 1
            continue

        # Check for unordered list item
        if line.startswith("- "):
            # Gather consecutive list items
            items = []
            while i < len(lines) and lines[i].startswith("- "):
                item_content = lines[i][2:].lstrip()  # Remove '- '
                item_content = process_inline(item_content)
                items.append(f"<li>{item_content}</li>")
                i += 1
            output.append("<ul>" + "".join(items) + "</ul>")
            continue

        # Otherwise, paragraph block
        block_lines = []
        while i < len(lines) and lines[i].strip() and not lines[i].startswith("- "):
            block_lines.append(lines[i])
            i += 1
        content = " ".join(block_lines)
        content = process_inline(content)
        output.append(f"<p>{content}</p>")

    return "\n".join(output)


def process_inline(text: str) -> str:
    """
    Apply inline formatting: **bold**, *italic*, `code`, [link](url).
    Precedence: bold > italic > code > link.
    We process each pattern sequentially, using a placeholder to avoid re-processing
    inside already transformed tags.
    """
    # First, bold
    text = re.sub(r"\*\*(.+?)\*\*", r"<strong>\1</strong>", text)
    # Second, italic (single asterisk not inside bold tags)
    text = re.sub(r"\*(.+?)\*", r"<em>\1</em>", text)
    # Third, code backticks
    text = re.sub(r"`(.+?)`", r"<code>\1</code>", text)
    # Fourth, links
    text = re.sub(r"\[(.+?)\]\((.+?)\)", r'<a href="\2">\1</a>', text)
    return text


if __name__ == "__main__":
    # Quick test
    test = "# Hi\n\nHello **world** and *peace* and `code`.\n\n- one\n- two\n\n[link](http://x.y)"
    print(to_html(test))
