from billing import compute_total
from cart import items

def run():
    print("total:", compute_total(items()))

if __name__ == "__main__":
    run()
