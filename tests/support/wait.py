import time


def wait_for(check, timeout=30.0, interval=0.5, message="condition"):
    """Call check() until it returns something truthy or the timeout passes.

    check() may raise; the exception counts as "not yet". Used for waiting on
    the stack to come up, never as a pause in the middle of a test.
    """
    deadline = time.monotonic() + timeout
    last_error = None
    while time.monotonic() < deadline:
        try:
            result = check()
            if result:
                return result
        except Exception as exc:  # noqa: BLE001 - any failure means "not ready"
            last_error = exc
        time.sleep(interval)
    raise TimeoutError(f"timed out after {timeout}s waiting for {message}; last error: {last_error!r}")
