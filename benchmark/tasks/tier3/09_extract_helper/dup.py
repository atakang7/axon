def _report(items):
    s = "REPORT:"
    s += "\n========"
    for item in items:
        s += f"\n- {item}"
    s += "\n========"
    return s


def report_users(users):
    return _report(users)


def report_orders(orders):
    return _report(orders)


def report_errors(errs):
    return _report(errs)
