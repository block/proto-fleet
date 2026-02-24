"""Type conversion utilities.

This module provides safe type conversion functions for common type transformations
used in SDK implementations.
"""

from __future__ import annotations

from proto_fleet_sdk.errors import InvalidConfigError

__all__ = ["safe_uint_to_int32", "safe_index_conversion"]

INT32_MAX = 2**31 - 1


def safe_uint_to_int32(value: int, field_name: str = "value") -> int:
    """Safely convert unsigned integer to signed int32.

    Example:
        >>> safe_uint_to_int32(1000)
        1000
    """
    if value < 0:
        raise InvalidConfigError(f"{field_name} cannot be negative")

    if value > INT32_MAX:
        raise InvalidConfigError(
            f"{field_name} exceeds int32 maximum ({INT32_MAX})"
        )

    return value


def safe_index_conversion(index: int, field_name: str = "index") -> int:
    """Safely convert component index to int32.

    Hardware indices (hashboards, ASICs, PSUs, fans) are bounded by physical constraints,
    so this conversion should always succeed in practice.

    Example:
        >>> safe_index_conversion(5)
        5
    """
    return safe_uint_to_int32(index, field_name)
