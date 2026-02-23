"""Unit conversion utilities for telemetry.

This module provides utility functions for converting between different units commonly
used in mining telemetry, particularly hashrate and power efficiency units.
"""

from __future__ import annotations

__all__ = [
    "ths_to_hs",
    "jth_to_jh",
    "hs_to_ths",
    "jh_to_jth",
]

# Conversion constants
THS_TO_HS_MULTIPLIER = 1e12  # TH/s to H/s
JTH_TO_JH_MULTIPLIER = 1e-12  # J/TH to J/H


def ths_to_hs(ths: float) -> float:
    """Convert terahashes per second to hashes per second.

    Example:
        >>> ths_to_hs(110.5)
        110500000000000.0
    """
    return ths * THS_TO_HS_MULTIPLIER


def jth_to_jh(jth: float) -> float:
    """Convert joules per terahash to joules per hash.

    Example:
        >>> jth_to_jh(25.0)
        2.5e-11
    """
    return jth * JTH_TO_JH_MULTIPLIER


def hs_to_ths(hs: float) -> float:
    """Convert hashes per second to terahashes per second.

    Example:
        >>> hs_to_ths(110500000000000.0)
        110.5
    """
    return hs / THS_TO_HS_MULTIPLIER


def jh_to_jth(jh: float) -> float:
    """Convert joules per hash to joules per terahash.

    Example:
        >>> jh_to_jth(2.5e-11)
        25.0
    """
    return jh / JTH_TO_JH_MULTIPLIER
