# Task: markdown → HTML (subset)

Write `md.py` exposing `to_html(md: str) -> str` supporting this subset:

- `# H1` / `## H2` / `### H3` (only at line start) → `<h1>`/`<h2>`/`<h3>`
- Paragraphs: blank-line-separated runs of text → `<p>...</p>`
- Inline `**bold**` → `<strong>`
- Inline `*italic*` → `<em>` (but `**` takes precedence)
- Inline `` `code` `` → `<code>`
- Unordered lists: lines starting with `- ` (consecutive) → `<ul><li>...</li></ul>`
- Links: `[text](url)` → `<a href="url">text</a>`

No external libraries. No nested lists, no images, no code fences.

## Success
- `python3 -c "from md import to_html; print(to_html('# Hi\n\nHello **world** and *peace* and \`code\`.\n\n- one\n- two\n\n[link](http://x.y)'))"` produces HTML containing each of: `<h1>Hi</h1>`, `<strong>world</strong>`, `<em>peace</em>`, `<code>code</code>`, `<ul>`, `<li>one</li>`, `<li>two</li>`, `<a href="http://x.y">link</a>`.
- Output for that input passes `python3 -c "import sys,html.parser as h; p=h.HTMLParser(); p.feed(sys.stdin.read())"` without errors (well-formed enough to parse).
