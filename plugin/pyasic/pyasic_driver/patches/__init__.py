"""Monkey-patches for pyasic bugs.

Applied at plugin startup before any pyasic operations. Patches are organized
by miner type/domain in separate modules.
"""

from __future__ import annotations

import logging

from pyasic_driver.patches import btminer

logger = logging.getLogger(__name__)

_applied = False


def apply() -> None:
    """Apply all pyasic patches. Idempotent."""
    global _applied  # noqa: PLW0603
    if _applied:
        return

    btminer.apply()
    _applied = True
    logger.info("Applied all pyasic patches")
