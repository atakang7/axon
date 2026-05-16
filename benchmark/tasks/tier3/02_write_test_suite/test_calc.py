import pytest
import calc


class TestDivide:
    def test_divide_normal_case(self):
        """Test normal division case"""
        result = calc.divide(10, 2)
        assert result == 5.0

    def test_divide_negative_numerator(self):
        """Test division with negative numerator"""
        result = calc.divide(-10, 2)
        assert result == -5.0

    def test_divide_by_zero_raises_value_error(self):
        """Test division by zero raises ValueError"""
        with pytest.raises(ValueError) as exc_info:
            calc.divide(10, 0)
        assert str(exc_info.value) == "division by zero"

    def test_divide_truncation_to_four_decimals(self):
        """Test division result is truncated to 4 decimals"""
        # Test case where division produces more than 4 decimals
        result = calc.divide(1, 3)
        assert result == 0.3333  # 1/3 ≈ 0.333333..., rounded to 4 decimals

        # Test case where division produces exactly 4 decimals
        result = calc.divide(1, 8)
        assert result == 0.1250  # 1/8 = 0.125, represented as 0.1250

        # Test case where rounding occurs
        result = calc.divide(1, 7)
        assert result == 0.1429  # 1/7 ≈ 0.142857..., rounded to 0.1429


class TestIsPrime:
    def test_is_prime_edge_cases(self):
        """Test edge cases 0, 1, 2, 3, 4"""
        # 0 and 1 are not prime (less than 2)
        assert calc.is_prime(0) == False
        assert calc.is_prime(1) == False
        # 2 is prime
        assert calc.is_prime(2) == True
        # 3 is prime
        assert calc.is_prime(3) == True
        # 4 is composite
        assert calc.is_prime(4) == False

    def test_is_prime_large_prime(self):
        """Test a large prime number (e.g. 7919)"""
        # 7919 is a known prime number
        assert calc.is_prime(7919) == True

    def test_is_prime_large_composite(self):
        """Test a large composite number"""
        # 7918 is even, so composite (7918 = 2 * 3959)
        assert calc.is_prime(7918) == False
        # Also test another composite: 7921 = 89 * 89
        assert calc.is_prime(7921) == False

    def test_is_prime_non_int_returns_false(self):
        """Test non-integer input returns False"""
        # Test with float
        assert calc.is_prime(5.0) == False
        # Test with string
        assert calc.is_prime("5") == False
        # Test with None
        assert calc.is_prime(None) == False
