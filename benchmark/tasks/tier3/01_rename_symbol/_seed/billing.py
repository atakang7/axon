from tax import apply_tax

def compute_total(items):
    subtotal = sum(p * q for p, q in items)
    return apply_tax(subtotal)

def describe():
    return "compute_total: returns post-tax total"
