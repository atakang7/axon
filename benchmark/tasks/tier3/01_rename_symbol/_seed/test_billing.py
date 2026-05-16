from billing import compute_total

def test_compute_total():
    assert compute_total([(1.0, 1)]) == 1.20
