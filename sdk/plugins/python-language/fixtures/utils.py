"""
Utility functions — demonstrates generators, context managers, decorators, and constants.
"""
from __future__ import annotations

import functools
import time
from contextlib import contextmanager
from typing import Any, Callable, Generator, Iterator, TypeVar

F = TypeVar("F", bound=Callable[..., Any])

DEFAULT_RETRIES = 3
DEFAULT_BACKOFF = 0.5
MAX_BACKOFF     = 30.0


def retry(max_attempts: int = DEFAULT_RETRIES, backoff: float = DEFAULT_BACKOFF):
    """Decorator that retries a function on exception with exponential backoff."""
    def decorator(fn: F) -> F:
        @functools.wraps(fn)
        def wrapper(*args: Any, **kwargs: Any) -> Any:
            delay = backoff
            for attempt in range(1, max_attempts + 1):
                try:
                    return fn(*args, **kwargs)
                except Exception as exc:
                    if attempt == max_attempts:
                        raise
                    time.sleep(min(delay, MAX_BACKOFF))
                    delay *= 2
        return wrapper  # type: ignore[return-value]
    return decorator


def memoize(fn: F) -> F:
    """Simple in-memory memoization decorator."""
    cache: dict[tuple, Any] = {}

    @functools.wraps(fn)
    def wrapper(*args: Any) -> Any:
        if args not in cache:
            cache[args] = fn(*args)
        return cache[args]
    return wrapper  # type: ignore[return-value]


@contextmanager
def timer(label: str = "") -> Generator[None, None, None]:
    """Context manager that logs elapsed time."""
    start = time.perf_counter()
    try:
        yield
    finally:
        elapsed = time.perf_counter() - start
        print(f"{label}: {elapsed:.3f}s" if label else f"elapsed: {elapsed:.3f}s")


class Paginator:
    """Iterable that paginates a sequence into fixed-size pages."""

    def __init__(self, items: list, page_size: int = 20) -> None:
        self._items = items
        self._page_size = page_size

    def __iter__(self) -> Iterator[list]:
        for i in range(0, len(self._items), self._page_size):
            yield self._items[i : i + self._page_size]

    @property
    def page_count(self) -> int:
        if not self._items:
            return 0
        return (len(self._items) + self._page_size - 1) // self._page_size


def chunk_generator(iterable: list, size: int) -> Generator[list, None, None]:
    """Yield successive fixed-size chunks from a list."""
    for i in range(0, len(iterable), size):
        yield iterable[i : i + size]


def flatten(nested: list[list]) -> list:
    """Flatten one level of nesting."""
    return [item for sublist in nested for item in sublist]


def deep_merge(base: dict, override: dict) -> dict:
    """Recursively merge override into base, returning a new dict."""
    result = dict(base)
    for key, value in override.items():
        if key in result and isinstance(result[key], dict) and isinstance(value, dict):
            result[key] = deep_merge(result[key], value)
        else:
            result[key] = value
    return result
