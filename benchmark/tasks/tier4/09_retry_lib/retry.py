import time
from functools import wraps


def retry(times=3, exceptions=(Exception,), backoff=0.0, on_giveup=None):
    """
    Decorator that retries a function on specified exceptions.

    Args:
        times: total attempts (1 try + times-1 retries). Default 3.
        exceptions: tuple of exception types to catch and retry. Default (Exception,).
        backoff: seconds to sleep between attempts = backoff * attempt number. Default 0.0.
        on_giveup: callable(exc) called after final failure before raising.
    """
    if times < 1:
        raise ValueError("times must be >= 1")

    def decorator(func):
        @wraps(func)
        def wrapper(*args, **kwargs):
            last_exc = None
            for attempt in range(1, times + 1):
                try:
                    return func(*args, **kwargs)
                except exceptions as exc:
                    last_exc = exc
                    if attempt < times:
                        time.sleep(backoff * attempt)
                    # else: will raise after loop
                # Other exceptions propagate immediately (not caught)
            # exhausted attempts
            if on_giveup is not None:
                on_giveup(last_exc)
            raise last_exc

        return wrapper

    return decorator
