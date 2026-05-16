def greet(name, lang="en"):
<<<<<<< HEAD
    if lang == "en":
        return f"Hello, {name}!"
    if lang == "es":
        return f"Hola, {name}!"
    return f"Hi, {name}"
=======
    greetings = {"en": "Hello", "es": "Hola", "fr": "Bonjour"}
    word = greetings.get(lang, "Hi")
    return f"{word}, {name}!"
>>>>>>> feature/more-langs

def shout(name):
    return greet(name).upper()
