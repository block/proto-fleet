"""Patches for pyasic's VNish backend.

Fixes compatibility issues with VNish firmware API responses that differ
from what pyasic expects.
"""

from __future__ import annotations

import logging
from typing import Any

logger = logging.getLogger(__name__)

_applied = False


def apply() -> None:
    """Apply all VNish patches. Safe to call multiple times."""
    global _applied  # noqa: PLW0603
    if _applied:
        return
    _patch_get_config()
    _patch_set_power_limit()
    _applied = True
    logger.info("Applied VNish patches")


def _patch_get_config() -> None:
    """Fix get_config to handle autotune_presets returning a list.

    Some VNish firmware versions return the presets array directly from
    /api/v1/autotune/presets instead of wrapping it in {"presets": [...]}.
    pyasic calls .get("presets") on the response, which fails on a list.
    """
    from pyasic.config import MinerConfig
    from pyasic.errors import APIError
    from pyasic.miners.backends.vnish import VNish

    async def get_config_fixed(self: Any) -> MinerConfig:
        try:
            web_settings = await self.web.settings()
            web_presets_raw = await self.web.autotune_presets()
            if isinstance(web_presets_raw, list):
                web_presets = web_presets_raw
            elif isinstance(web_presets_raw, dict):
                web_presets = web_presets_raw.get("presets", [])
            else:
                web_presets = []
            web_perf_summary = (await self.web.perf_summary()) or {}
        except APIError:
            return self.config or MinerConfig()

        try:
            self.config = MinerConfig.from_vnish(
                web_settings, web_presets, web_perf_summary
            )
        except Exception:
            logger.warning("Failed to parse VNish config for %s, using cached", self.ip, exc_info=True)
            return self.config or MinerConfig()

        return self.config

    VNish.get_config = get_config_fixed  # type: ignore[assignment]
    logger.debug("Patched VNish.get_config (handle list autotune_presets + parse errors)")


def _patch_set_power_limit() -> None:
    """Fix set_power_limit to work regardless of current mining mode.

    Stock pyasic bails if the miner isn't already in preset mode, and has a
    brittle verification step. This patch:
    1. Fetches available presets directly from the API (works even in manual mode)
    2. Picks the best preset <= requested wattage
    3. Calls web.set_power_limit which switches the miner to that preset
    4. Skips the read-back verification that fails on some firmware versions
    """
    from pyasic.errors import APIError
    from pyasic.miners.backends.vnish import VNish

    async def set_power_limit_fixed(self: Any, wattage: int) -> bool:
        # Fetch presets directly from API — works regardless of current mode
        try:
            raw = await self.web.autotune_presets()
        except APIError:
            return False

        presets = raw if isinstance(raw, list) else raw.get("presets", []) if isinstance(raw, dict) else []

        valid_powers: list[int] = []
        for p in presets:
            if p.get("status") != "tuned":
                continue
            try:
                pw = int(p["pretty"].split("~")[0].replace("watt", "").strip())
            except (KeyError, ValueError, IndexError):
                continue
            if pw <= wattage:
                valid_powers.append(pw)

        if not valid_powers:
            logger.warning(
                "set_power_limit on %s: no tuned presets <= %dW",
                self.ip, wattage,
            )
            return False

        new_wattage = max(valid_powers)

        try:
            await self.web.set_power_limit(new_wattage)
        except APIError:
            raise
        except Exception as exc:
            logger.warning("Failed to set power limit on %s: %s", self.ip, exc)
            return False

        return True

    VNish.set_power_limit = set_power_limit_fixed  # type: ignore[assignment]
    logger.debug("Patched VNish.set_power_limit (fetch presets from API, skip verification)")
