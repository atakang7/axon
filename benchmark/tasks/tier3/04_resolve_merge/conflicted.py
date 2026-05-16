def greet(name, lang="en"):
    greetings = {"en": "Hello", "es": "Hola", "fr": "Bonjour"}
    word = greetings.get(lang, "Hi")
    if lang in greetings:
        return f"{word}, {name}!"
    else:
        return f"{word}, {name}"


def shout(name):
    return greet(name).upper()
