from mod_a import another_used

def caller():
    return another_used(5)

def orphan():
    return "i am never called"
