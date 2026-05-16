import re


def process_inline_old(text: str) -> str:
    text = re.sub(r"\*\*(.*?)\*\*", r"<strong>\1</strong>", text)
    text = re.sub(r"\*(.*?)\*", r"<em>\1</em>", text)
    text = re.sub(r"`(.*?)`", r"<code>\1</code>", text)
    text = re.sub(r"\[(.*?)\]\((.*?)\)", r'<a href="\2">\1</a>', text)
    return text


def process_inline_new(text: str) -> str:
    # Process bold first
    text = re.sub(r"\*\*(.+?)\*\*", r"<strong>\1</strong>", text)
    # Process italic (single asterisk) but not inside bold tags
    text = re.sub(r"\*(.+?)\*", r"<em>\1</em>", text)
    # Process code backticks
    text = re.sub(r"`(.+?)`", r"<code>\1</code>", text)
    # Process links
    text = re.sub(r"\[(.+?)\]\((.+?)\)", r'<a href="\2">\1</a>', text)
    return text


test = "Hello **world** and *peace* and `code`."
print("Old:", process_inline_old(test))
print("New:", process_inline_new(test))
