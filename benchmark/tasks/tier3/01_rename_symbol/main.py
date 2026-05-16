from billing import compute_grand_total
from cart import items


def run():
    print("total:", compute_grand_total(items()))


if __name__ == "__main__":
    run()
