"""Port parsing and validation utilities.

This module provides functions for parsing and validating network port numbers.
"""

from __future__ import annotations

from proto_fleet_sdk.errors import InvalidConfigError
from proto_fleet_sdk.types import MAX_PORT

__all__ = ["parse_port"]

MIN_PORT = 1


def parse_port(port: str) -> int:
    """Parse and validate a port string.

    Example:
        >>> parse_port("8080")
        8080
    """
    try:
        port_int = int(port)
    except ValueError as e:
        raise InvalidConfigError(
            f"port must be a number between {MIN_PORT} and {MAX_PORT}",
            cause=e,
        ) from e

    if port_int < MIN_PORT or port_int > MAX_PORT:
        raise InvalidConfigError(
            f"port must be between {MIN_PORT} and {MAX_PORT}, got {port_int}"
        )

    return port_int
