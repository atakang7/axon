#!/usr/bin/env python3
from vending import Vending


def test_initial_state():
    v = Vending()
    assert v.state == Vending.IDLE, f"Expected IDLE, got {v.state}"
    print("✓ Initial state is IDLE")


def test_insert_coin_from_idle():
    v = Vending()
    result = v.insert_coin()
    assert v.state == Vending.HAS_COIN, f"Expected HAS_COIN, got {v.state}"
    assert result == Vending.HAS_COIN, f"Expected HAS_COIN, got {result}"
    print("✓ insert_coin from IDLE → HAS_COIN")


def test_insert_coin_from_has_coin():
    v = Vending()
    v.insert_coin()  # First coin
    result = v.insert_coin()  # Second coin
    assert v.state == Vending.HAS_COIN, f"Expected HAS_COIN, got {v.state}"
    assert result == Vending.HAS_COIN, f"Expected HAS_COIN, got {result}"
    print("✓ insert_coin from HAS_COIN stays HAS_COIN")


def test_insert_coin_from_dispensing():
    v = Vending()
    v.insert_coin()
    v.select("soda")  # Now in DISPENSING
    try:
        v.insert_coin()
        assert False, "Should have raised ValueError"
    except ValueError as e:
        print(f"✓ insert_coin from DISPENSING raises ValueError: {e}")


def test_select_from_has_coin():
    v = Vending()
    v.insert_coin()
    result = v.select("soda")
    assert v.state == Vending.DISPENSING, f"Expected DISPENSING, got {v.state}"
    assert result == Vending.DISPENSING, f"Expected DISPENSING, got {result}"
    print("✓ select from HAS_COIN → DISPENSING")


def test_select_from_idle():
    v = Vending()
    try:
        v.select("soda")
        assert False, "Should have raised ValueError"
    except ValueError as e:
        print(f"✓ select from IDLE raises ValueError: {e}")


def test_select_from_dispensing():
    v = Vending()
    v.insert_coin()
    v.select("soda")  # Now in DISPENSING
    try:
        v.select("soda")
        assert False, "Should have raised ValueError"
    except ValueError as e:
        print(f"✓ select from DISPENSING raises ValueError: {e}")


def test_dispense_from_dispensing():
    v = Vending()
    v.insert_coin()
    v.select("soda")  # Now in DISPENSING
    result = v.dispense()
    assert v.state == Vending.IDLE, f"Expected IDLE, got {v.state}"
    assert result == Vending.IDLE, f"Expected IDLE, got {result}"
    print("✓ dispense from DISPENSING → IDLE")


def test_dispense_from_idle():
    v = Vending()
    try:
        v.dispense()
        assert False, "Should have raised ValueError"
    except ValueError as e:
        print(f"✓ dispense from IDLE raises ValueError: {e}")


def test_dispense_from_has_coin():
    v = Vending()
    v.insert_coin()  # Now in HAS_COIN
    try:
        v.dispense()
        assert False, "Should have raised ValueError"
    except ValueError as e:
        print(f"✓ dispense from HAS_COIN raises ValueError: {e}")


def test_full_cycle():
    v = Vending()
    v.insert_coin()
    v.select("candy")
    v.dispense()
    assert v.state == Vending.IDLE, f"Expected IDLE after full cycle, got {v.state}"
    print("✓ Full cycle IDLE → HAS_COIN → DISPENSING → IDLE works")


if __name__ == "__main__":
    test_initial_state()
    test_insert_coin_from_idle()
    test_insert_coin_from_has_coin()
    test_insert_coin_from_dispensing()
    test_select_from_has_coin()
    test_select_from_idle()
    test_select_from_dispensing()
    test_dispense_from_dispensing()
    test_dispense_from_idle()
    test_dispense_from_has_coin()
    test_full_cycle()
    print("\n✅ All tests passed!")
