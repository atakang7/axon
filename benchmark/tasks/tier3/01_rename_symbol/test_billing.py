from billing import compute_grand_total


def test_compute_grand_total():
    assert compute_grand_total([(1.0, 1)]) == 1.20
