"""Patches for pyasic's WhatsMiner (BTMiner) backend.

We force the V2 backend (port 4028) for all WhatsMiner devices because the
V3 backend has many unresolved issues. This module also fixes model detection,
multicommand truncation, hashboard parsing, and silent send_config failures.
"""

from __future__ import annotations

import logging
from typing import Any

logger = logging.getLogger(__name__)


class BTMinerV3AuthError(Exception):
    """Raised when a BTMinerV3 RPC command fails due to incorrect credentials."""

_applied = False


def apply() -> None:
    """Apply all WhatsMiner patches. Safe to call multiple times."""
    global _applied  # noqa: PLW0603
    if _applied:
        return
    _patch_factory_v3_probe()
    _patch_factory_model_detection()
    _patch_force_v2()
    _patch_multicommand()
    _patch_hashboards()
    _patch_privileged_commands()
    _patch_send_config()
    _patch_error_codes()
    _patch_auth_error()
    _patch_validate_write_access()
    _applied = True
    logger.info("Applied WhatsMiner (BTMiner) patches")


def _patch_factory_v3_probe() -> None:
    """Skip V3 probe — we force V2, so the V3 probe on port 4433 is
    unnecessary and crashes on IncompleteReadError.
    """
    from pyasic.miners.factory import MinerFactory

    async def send_btminer_v3_api_command_noop(self: Any, ip: str, command: str) -> dict | None:
        return None

    MinerFactory.send_btminer_v3_api_command = send_btminer_v3_api_command_noop  # type: ignore[assignment]
    logger.debug("Patched MinerFactory.send_btminer_v3_api_command (skip V3 probe)")


def _patch_factory_model_detection() -> None:
    """Fall back to get_version RPC for model detection when devdetails (V2)
    and get.device.info (V3) both fail on newer firmware.
    """
    from pyasic.miners.factory import MinerFactory

    original_fn = MinerFactory.get_miner_model_whatsminer

    async def get_miner_model_whatsminer_fixed(self: Any, ip: str) -> str | None:
        result = await original_fn(self, ip)
        if result is not None:
            return result

        # get_version returns miner_type on all firmware.
        # e.g. "M60S_VK40" → "M60SVK40" (strip underscores to match pyasic keys)
        try:
            version_data = await self.send_api_command(ip, "get_version")
        except Exception:
            return None
        if version_data is not None:
            try:
                miner_type = version_data["Msg"]["miner_type"]
                return miner_type.replace("_", "")
            except (TypeError, LookupError):
                pass
        return None

    MinerFactory.get_miner_model_whatsminer = get_miner_model_whatsminer_fixed  # type: ignore[assignment]
    logger.debug("Patched MinerFactory.get_miner_model_whatsminer (get_version fallback)")


def _patch_force_v2() -> None:
    """Force V2 backend for all WhatsMiner devices. The V3 backend has broken
    is_mining, send_config, get_errors, upgrade_firmware, set.miner.pools,
    timeout handling, and empty-response parsing.
    """
    from pyasic.miners.backends.btminer import BTMiner

    original_new = BTMiner.__new__

    def new_force_v2(cls: type, ip: str, version: str | None = None) -> Any:
        return original_new(cls, ip, None)  # None forces BTMinerV2

    BTMiner.__new__ = new_force_v2  # type: ignore[assignment]
    logger.debug("Patched BTMiner.__new__ (force V2 for all WhatsMiner devices)")


def _patch_multicommand() -> None:
    """Send all commands individually and normalize summary responses.

    Joined multicommand ("summary+devs+pools") returns truncated SUMMARY
    (4 fields, missing telemetry). Individual summary uses WhatsMiner Msg
    format instead of cgminer SUMMARY format. We split all commands and
    convert Msg → SUMMARY.
    """
    from pyasic.rpc.btminer import BTMinerRPCAPI

    async def multicommand_all_split(
        self: Any, *commands: str, allow_warning: bool = True
    ) -> dict:
        checked = self._check_commands(*commands)
        data = await self._send_split_multicommand(*checked, allow_warning=allow_warning)

        # Normalize WhatsMiner Msg format → cgminer SUMMARY format.
        if "summary" in data and len(data["summary"]) > 0:
            resp = data["summary"][0]
            if "Msg" in resp and isinstance(resp["Msg"], dict) and "SUMMARY" not in resp:
                data["summary"] = [
                    {
                        "STATUS": [{"STATUS": resp.get("STATUS", "S"), "Msg": "Summary"}],
                        "SUMMARY": [resp["Msg"]],
                        "id": 1,
                    }
                ]

        data["multicommand"] = True
        return data

    BTMinerRPCAPI.multicommand = multicommand_all_split  # type: ignore[assignment]
    logger.debug("Patched BTMinerRPCAPI.multicommand (split all + normalize summary)")


def _patch_hashboards() -> None:
    """Fix hashboard parsing: use .get() for optional Chip Temp Avg, and
    detect mislabeled "MHS 1m" units (actually TH/s on newer firmware)
    by comparing with Factory GHS.
    """
    from pyasic.data import HashBoard
    from pyasic.errors import APIError
    from pyasic.miners.backends.btminer import BTMinerV2

    async def _get_hashboards_fixed(self: Any, rpc_devs: dict | None = None) -> list[HashBoard]:
        if self.expected_hashboards is None:
            return []

        hashboards = [
            HashBoard(slot=i, expected_chips=self.expected_chips)
            for i in range(self.expected_hashboards)
        ]

        if rpc_devs is None:
            try:
                rpc_devs = await self.rpc.devs()
            except APIError:
                pass

        if rpc_devs is not None:
            for board in rpc_devs.get("DEVS", []):
                try:
                    asc = board.get("ASC")
                    if asc is None:
                        asc = board["Slot"]
                    while len(hashboards) < asc + 1:
                        hashboards.append(
                            HashBoard(slot=len(hashboards), expected_chips=self.expected_chips)
                        )
                    self.expected_hashboards = len(hashboards)
                    chip_temp = board.get("Chip Temp Avg")
                    if chip_temp is not None:
                        hashboards[asc].chip_temp = round(chip_temp)
                    hashboards[asc].temp = round(board["Temperature"])

                    # Devs "MHS 1m" is in TH/s on newer firmware (mislabeled).
                    # Detect by comparing with Factory GHS.
                    mhs = float(board["MHS 1m"])
                    factory_ghs = board.get("Factory GHS", 0)
                    if factory_ghs > 0 and mhs < factory_ghs:
                        unit = self.algo.unit.default  # TH/s
                    else:
                        unit = self.algo.unit.MH
                    hashboards[asc].hashrate = self.algo.hashrate(
                        rate=mhs, unit=unit,
                    ).into(self.algo.unit.default)

                    hashboards[asc].chips = board["Effective Chips"]
                    hashboards[asc].serial_number = board["PCB SN"]
                    hashboards[asc].missing = False
                except LookupError:
                    pass

        return hashboards

    BTMinerV2._get_hashboards = _get_hashboards_fixed  # type: ignore[assignment]
    logger.debug("Patched BTMinerV2._get_hashboards (optional Chip Temp Avg)")


def _patch_privileged_commands() -> None:
    """Treat empty responses and APIErrors as success for reboot, stop_mining,
    resume_mining, and restart_backend — the miner drops the connection
    mid-response so pyasic parses it as {} and returns False.
    """
    from pyasic.errors import APIError
    from pyasic.miners.backends.btminer import BTMinerV2

    def _make_fixed(rpc_method_name: str, **rpc_kwargs: Any) -> Any:
        async def fixed(self: Any) -> bool:
            try:
                data = await getattr(self.rpc, rpc_method_name)(**rpc_kwargs)
            except APIError as exc:
                if "auth" in str(exc).lower() or "password" in str(exc).lower():
                    raise
                return True
            if not data:
                return True
            if data.get("Msg") == "API command OK":
                return True
            return False
        return fixed

    BTMinerV2.reboot = _make_fixed("reboot")  # type: ignore[assignment]
    BTMinerV2.stop_mining = _make_fixed("power_off", respbefore=True)  # type: ignore[assignment]
    BTMinerV2.resume_mining = _make_fixed("power_on")  # type: ignore[assignment]
    BTMinerV2.restart_backend = _make_fixed("restart")  # type: ignore[assignment]
    logger.debug("Patched BTMinerV2 privileged commands (empty response = success)")


def _patch_send_config() -> None:
    """Let APIError from update_pools propagate instead of being silently
    swallowed. Only catch APIError for power mode commands.
    """
    from pyasic.config import MinerConfig
    from pyasic.errors import APIError
    from pyasic.miners.backends.btminer import BTMinerV2

    async def send_config_fixed(
        self: BTMinerV2, config: MinerConfig, user_suffix: str | None = None
    ) -> None:
        self.config = config

        conf = config.as_wm(user_suffix=user_suffix)
        pools_conf = conf["pools"]

        resp = await self.rpc.update_pools(**pools_conf)
        logger.debug("update_pools response for %s: %s", self.ip, resp)

        try:
            if conf["mode"] == "normal":
                await self.rpc.set_normal_power()
            elif conf["mode"] == "high":
                await self.rpc.set_high_power()
            elif conf["mode"] == "low":
                await self.rpc.set_low_power()
            elif conf["mode"] == "power_tuning":
                await self.rpc.adjust_power_limit(conf["power_tuning"]["wattage"])
        except APIError as exc:
            logger.warning("Power mode command failed for %s: %s", self.ip, exc)

    BTMinerV2.send_config = send_config_fixed  # type: ignore[assignment]
    logger.debug("Patched BTMinerV2.send_config (propagate pool update errors)")


def _patch_error_codes() -> None:
    """Add WhatsMiner numeric error code mapping to device error inference.

    Skips gracefully if device.py does not yet export _infer_miner_error.
    """
    import pyasic_driver.device as device_mod

    if not hasattr(device_mod, "_infer_miner_error"):
        logger.debug("Skipped _patch_error_codes (_infer_miner_error not found in device.py)")
        return

    from proto_fleet_sdk.error_codes import MinerError

    _CODE_RANGE_TO_MINER_ERROR: list[tuple[int, int, MinerError]] = [
        (100, 100, MinerError.FAN_FAILED),
        (110, 111, MinerError.FAN_SPEED_DEVIATION),
        (200, 200, MinerError.PSU_NOT_PRESENT),
        (201, 201, MinerError.PSU_MODEL_MISMATCH),
        (202, 202, MinerError.PSU_OUTPUT_VOLTAGE_FAULT),
        (203, 204, MinerError.PSU_OVER_TEMPERATURE),
        (205, 205, MinerError.PSU_OUTPUT_OVERCURRENT),
        (206, 206, MinerError.PSU_INPUT_VOLTAGE_LOW),
        (207, 207, MinerError.PSU_OUTPUT_OVERCURRENT),
        (208, 219, MinerError.PSU_FAULT_GENERIC),
        (300, 309, MinerError.TEMP_SENSOR_OPEN_OR_SHORT),
        (320, 328, MinerError.TEMP_SENSOR_FAULT),
        (329, 329, MinerError.CONTROL_BOARD_COMMUNICATION_LOST),
        (400, 400, MinerError.EEPROM_READ_FAILURE),
        (500, 500, MinerError.HASHBOARD_NOT_PRESENT),
        (510, 519, MinerError.HASHBOARD_NOT_PRESENT),
        (600, 600, MinerError.DEVICE_OVER_TEMPERATURE),
        (610, 610, MinerError.DEVICE_OVER_TEMPERATURE),
        (700, 700, MinerError.CONTROL_BOARD_FAILURE),
        (701, 701, MinerError.CONTROL_BOARD_FAILURE),
        (710, 714, MinerError.CONTROL_BOARD_FAILURE),
        (800, 802, MinerError.FIRMWARE_IMAGE_INVALID),
        (2000, 2000, MinerError.VENDOR_ERROR_UNMAPPED),
        (2310, 2310, MinerError.HASHRATE_BELOW_TARGET),
        (5200, 5209, MinerError.ASIC_CRC_ERROR_EXCESSIVE),
        (5300, 5309, MinerError.HASHBOARD_MISSING_CHIPS),
        (5400, 5409, MinerError.HASHBOARD_ASIC_OVER_TEMPERATURE),
        (5500, 5509, MinerError.ASIC_CRC_ERROR_EXCESSIVE),
        (5600, 5609, MinerError.HASHBOARD_MISSING_CHIPS),
    ]

    original_infer = device_mod._infer_miner_error

    def _infer_with_whatsminer_codes(
        error_message: str, error_code: int | None,
    ) -> MinerError:
        if error_code is not None:
            for lo, hi, miner_err in _CODE_RANGE_TO_MINER_ERROR:
                if lo <= error_code <= hi:
                    return miner_err
        return original_infer(error_message, error_code)

    device_mod._infer_miner_error = _infer_with_whatsminer_codes  # type: ignore[assignment]
    logger.debug("Patched _infer_miner_error (WhatsMiner numeric error codes)")


def _patch_auth_error() -> None:
    """Wrap BTMinerV2.get_data to convert BTMinerV3AuthError into
    AuthenticationFailedError so device.py stays manufacturer-agnostic.
    """
    from proto_fleet_sdk.errors import AuthenticationFailedError
    from pyasic.miners.backends.btminer import BTMinerV2

    original_get_data = BTMinerV2.get_data

    async def get_data_with_auth_check(self: Any, *args: Any, **kwargs: Any) -> Any:
        try:
            return await original_get_data(self, *args, **kwargs)
        except BTMinerV3AuthError as exc:
            raise AuthenticationFailedError(
                device_id=str(self.ip), cause=exc,
            ) from exc

    BTMinerV2.get_data = get_data_with_auth_check  # type: ignore[assignment]
    logger.debug("Patched BTMinerV2.get_data (BTMinerV3AuthError → AuthenticationFailedError)")


def _patch_validate_write_access() -> None:
    """Register WhatsMiner write credential validation.

    WhatsMiner uses separate auth for reads vs writes — get_data() succeeds
    with any password, but write operations require the correct one.
    """
    from pyasic.miners.backends.btminer import BTMinerV2

    from pyasic_driver.driver import register_write_validator

    async def _validate(miner: Any) -> None:
        config = await miner.get_config()
        if config is not None:
            await miner.send_config(config)

    register_write_validator(BTMinerV2, _validate)
    logger.debug("Registered WhatsMiner write credential validator")
