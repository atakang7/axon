def report_users(users):
    s = "REPORT:"
    s += "\n========"
    for u in users:
        s += f"\n- {u}"
    s += "\n========"
    return s

def report_orders(orders):
    s = "REPORT:"
    s += "\n========"
    for o in orders:
        s += f"\n- {o}"
    s += "\n========"
    return s

def report_errors(errs):
    s = "REPORT:"
    s += "\n========"
    for e in errs:
        s += f"\n- {e}"
    s += "\n========"
    return s
