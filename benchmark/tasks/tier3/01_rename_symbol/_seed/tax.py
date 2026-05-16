RATE = 0.20

def apply_tax(amount):
    return round(amount * (1 + RATE), 2)
