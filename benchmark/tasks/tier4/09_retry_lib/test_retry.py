import time
import pytest
from retry import retry


def test_succeeds_on_first_try():
    """Decorated function that works immediately should not retry."""
    call_count = 0

    @retry(times=3)
    def foo():
        nonlocal call_count
        call_count += 1
        return 42

    result = foo()
    assert result == 42
    assert call_count == 1


def test_succeeds_after_one_retry(monkeypatch):
    """Function raises once, then succeeds on second attempt."""
    monkeypatch.setattr(time, "sleep", lambda *_: None)
    call_count = 0

    @retry(times=3)
    def foo():
        nonlocal call_count
        call_count += 1
        if call_count == 1:
            raise ValueError("first")
        return "ok"

    result = foo()
    assert result == "ok"
    assert call_count == 2


def test_gives_up_after_times(monkeypatch):
    """Function raises each time; after `times` attempts, raise last exception."""
    monkeypatch.setattr(time, "sleep", lambda *_: None)
    call_count = 0

    @retry(times=3)
    def foo():
        nonlocal call_count
        call_count += 1
        raise RuntimeError(f"attempt {call_count}")

    with pytest.raises(RuntimeError) as exc_info:
        foo()
    assert str(exc_info.value) == "attempt 3"
    assert call_count == 3


def test_does_not_catch_unrelated_exceptions():
    """Exceptions not in `exceptions` tuple should propagate immediately."""
    call_count = 0

    @retry(times=3, exceptions=(ValueError,))
    def foo():
        nonlocal call_count
        call_count += 1
        raise KeyError("not caught")

    with pytest.raises(KeyError) as exc_info:
        foo()
    assert (
        str(exc_info.value) == "'not caught'"
    )  # KeyError('not caught') string repr includes quotes
    assert call_count == 1  # no retry


def test_calls_on_giveup(monkeypatch):
    """on_giveup callable is invoked with the last exception before raising."""
    monkeypatch.setattr(time, "sleep", lambda *_: None)
    call_count = 0
    giveup_called = []

    def on_giveup(exc):
        giveup_called.append(exc)

    @retry(times=2, on_giveup=on_giveup)
    def foo():
        nonlocal call_count
        call_count += 1
        raise TypeError("fail")

    with pytest.raises(TypeError) as exc_info:
        foo()
    assert str(exc_info.value) == "fail"
    assert call_count == 2
    assert len(giveup_called) == 1
    assert giveup_called[0] is exc_info.value


def test_backoff_respected(monkeypatch):
    """Sleep duration should be backoff * attempt."""
    sleeps = []
    monkeypatch.setattr(time, "sleep", lambda sec: sleeps.append(sec))
    call_count = 0

    @retry(times=4, backoff=0.5)
    def foo():
        nonlocal call_count
        call_count += 1
        raise ValueError("always")

    with pytest.raises(ValueError):
        foo()
    # attempts 1,2,3 sleep before retry; attempt 4 raises, no sleep after
    # sleep after attempt 1: backoff * 1 = 0.5
    # after attempt 2: backoff * 2 = 1.0
    # after attempt 3: backoff * 3 = 1.5
    expected = [0.5, 1.0, 1.5]
    assert sleeps == expected
    assert call_count == 4


def test_times_one_no_retry():
    """times=1 means exactly one attempt, no retry."""
    call_count = 0

    @retry(times=1)
    def foo():
        nonlocal call_count
        call_count += 1
        raise ValueError("once")

    with pytest.raises(ValueError):
        foo()
    assert call_count == 1


def test_exceptions_empty_tuple():
    """Empty exceptions tuple means no exception is caught -> immediate raise."""
    call_count = 0

    @retry(times=3, exceptions=())
    def foo():
        nonlocal call_count
        call_count += 1
        raise ValueError("test")

    with pytest.raises(ValueError):
        foo()
    assert call_count == 1
