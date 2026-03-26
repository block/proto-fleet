"""Generic PyASIC device implementation.

Single device class for ALL manufacturers supported by pyasic. Delegates all operations
to pyasic's unified API. No manufacturer-specific subclasses needed.

Returns proto types directly — no SDK type conversion layer.
"""

from __future__ import annotations

import asyncio
import logging
from collections.abc import Callable
from datetime import datetime, timezone
from typing import Any

from google.protobuf.timestamp_pb2 import Timestamp
from proto_fleet_sdk.error_codes import (
    ComponentType,
    MinerError,
    Severity,
)
from proto_fleet_sdk.errors import (
    DeviceUnavailableError,
    SDKError,
    UnsupportedCapabilityError,
)
from proto_fleet_sdk.generated.pb import driver_pb2
from pyasic.errors import APIError

logger = logging.getLogger(__name__)

# Capabilities are just dict[str, bool]
Capabilities = dict[str, bool]

_BLINK_LED_DURATION_SECS = 30
_DEFAULT_STRATUM_PASSWORD = "x"
_DISRUPTIVE_CONTROL_COMMANDS = frozenset({"resume_mining", "stop_mining", "reboot"})

# Unit conversion constants — inlined from the removed telemetry/converters.py
_THS_TO_HS = 1e12
_JTH_TO_JH = 1e-12


def _metric_gauge(value: float) -> driver_pb2.MetricValue:
    return driver_pb2.MetricValue(value=value, kind=driver_pb2.METRIC_KIND_GAUGE)


def _metric_rate(value: float) -> driver_pb2.MetricValue:
    return driver_pb2.MetricValue(value=value, kind=driver_pb2.METRIC_KIND_RATE)


def _has_value(val: object) -> bool:
    """Check if a value is present and non-zero.

    Uses float() conversion because pyasic may return rich objects
    (e.g. SHA256HashRate) that support float() but not comparison operators.
    """
    if val is None:
        return False
    try:
        return float(val) != 0  # type: ignore[arg-type]
    except (TypeError, ValueError):
        return False


def _to_float(val: object) -> float:
    """Convert a value known to be non-None to float."""
    return float(val)  # type: ignore[arg-type]


def _require_cap(caps: Capabilities, cap: str, device_id: str) -> None:
    if not caps.get(cap):
        raise UnsupportedCapabilityError(cap, device_id=device_id)


def _is_timeout_exception(exc: BaseException) -> bool:
    if isinstance(exc, TimeoutError):
        return True

    if exc.__class__.__name__ in {
        "ReadTimeout",
        "WriteTimeout",
        "ConnectTimeout",
        "PoolTimeout",
        "TimeoutException",
    }:
        return True

    nested = exc.__cause__ or exc.__context__
    return nested is not None and nested is not exc and _is_timeout_exception(nested)


_KEYWORD_TO_MINER_ERROR: list[tuple[str, MinerError]] = [
    ("fan", MinerError.FAN_FAILED),
    ("psu", MinerError.PSU_FAULT_GENERIC),
    ("power supply", MinerError.PSU_FAULT_GENERIC),
    ("over temperature", MinerError.DEVICE_OVER_TEMPERATURE),
    ("temperature is too high", MinerError.DEVICE_OVER_TEMPERATURE),
    ("overheat", MinerError.DEVICE_OVER_TEMPERATURE),
    ("hashboard", MinerError.HASHBOARD_NOT_PRESENT),
    ("hash board", MinerError.HASHBOARD_NOT_PRESENT),
    ("eeprom", MinerError.EEPROM_READ_FAILURE),
    ("control board", MinerError.CONTROL_BOARD_FAILURE),
    ("firmware", MinerError.FIRMWARE_IMAGE_INVALID),
]


def _infer_miner_error(error_message: str, error_code: int | None) -> MinerError:
    """Map a pyasic error to the most specific MinerError enum value."""
    msg_lower = error_message.lower()
    for keyword, miner_err in _KEYWORD_TO_MINER_ERROR:
        if keyword in msg_lower:
            return miner_err

    return MinerError.VENDOR_ERROR_UNMAPPED


class DeviceCommandFailedError(DeviceUnavailableError):
    """Raised when a pyasic command returns False, indicating silent failure.

    pyasic command methods (reboot, resume_mining, fault_light_on, etc.) return
    bool to indicate success/failure but never raise exceptions on failure.
    This error surfaces those silent failures to the fleet server.

    Extends DeviceUnavailableError so the gRPC servicer maps it to UNAVAILABLE
    rather than INTERNAL, matching the transient/retryable nature of the failure.
    """

    def __init__(self, command: str, device_id: str, *, miner_type: str = "", miner_ip: str = "") -> None:
        super().__init__(device_id=device_id)
        parts = [f"Command '{command}' failed on device {device_id}"]
        if miner_type or miner_ip:
            parts.append(f"(type={miner_type}, ip={miner_ip})")
        parts.append("— pyasic returned False.")
        msg = " ".join(parts)
        self.message = msg
        Exception.__init__(self, msg)


def _infer_severity(error_message: str) -> Severity:
    """Infer error severity from pyasic's translated error message."""
    msg = error_message.lower()
    critical_keywords = ["over temperature", "short", "protection", "fault", "failed", "overcurrent"]
    if any(kw in msg for kw in critical_keywords):
        return Severity.SEVERITY_CRITICAL
    minor_keywords = ["deviation", "warning", "ambient", "low"]
    if any(kw in msg for kw in minor_keywords):
        return Severity.SEVERITY_MINOR
    return Severity.SEVERITY_MAJOR


def _infer_component(error_message: str) -> ComponentType:
    """Infer component type from pyasic's translated error message."""
    msg = error_message.lower()
    if "fan" in msg:
        return ComponentType.COMPONENT_TYPE_FAN
    if any(kw in msg for kw in ("hashboard", "hash board", "chip", "asic", "chain")):
        return ComponentType.COMPONENT_TYPE_HASH_BOARD
    if any(kw in msg for kw in ("psu", "power supply", "power", "voltage", "current")):
        return ComponentType.COMPONENT_TYPE_PSU
    if any(kw in msg for kw in ("eeprom", "firmware", "checksum")):
        return ComponentType.COMPONENT_TYPE_EEPROM
    if any(kw in msg for kw in ("control board", "mac", "network")):
        return ComponentType.COMPONENT_TYPE_CONTROL_BOARD
    return ComponentType.COMPONENT_TYPE_UNSPECIFIED


def _determine_board_status(board: Any) -> int:
    """Returns proto ComponentStatus enum value."""
    hashrate = getattr(board, "hashrate", None)
    temp = getattr(board, "temp", None)
    chips = getattr(board, "chips", None)
    expected = getattr(board, "expected_chips", None)

    if _has_value(hashrate):
        if expected and chips and chips < expected:
            return driver_pb2.COMPONENT_STATUS_WARNING
        return driver_pb2.COMPONENT_STATUS_HEALTHY
    if _has_value(temp):
        return driver_pb2.COMPONENT_STATUS_WARNING
    return driver_pb2.COMPONENT_STATUS_OFFLINE


def _timestamp_from_datetime(dt: datetime) -> Timestamp:
    ts = Timestamp()
    ts.FromDatetime(dt)
    return ts


class PyAsicDevice:
    """Device wrapping a pyasic miner instance.

    Handles telemetry, control, pool configuration, and error reporting for any
    manufacturer that pyasic supports, using pyasic's unified async API.

    Returns proto types directly.
    """

    def __init__(
        self,
        device_id: str,
        miner: Any | None,
        device_info: driver_pb2.DeviceInfo,
        caps: Capabilities,
        cache_ttl_seconds: int = 5,
        probe_fn: Any | None = None,
        secret: Any | None = None,
        on_caps_update: Callable[[str, Capabilities], None] | None = None,
    ) -> None:
        self._id = device_id
        self._miner = miner
        self._info = device_info
        self._caps = caps
        self._cache_ttl_seconds = cache_ttl_seconds
        self._probe_fn = probe_fn
        self._secret = secret
        self._on_caps_update = on_caps_update
        self._last_status: driver_pb2.DeviceMetrics | None = None
        self._last_status_at: datetime | None = None

    def id(self) -> str:
        return self._id

    async def _ensure_connected(self) -> bool:
        """Ensure we have a live pyasic miner connection. Returns True if connected."""
        if self._miner is not None:
            return True
        if self._probe_fn is None:
            return False
        try:
            miner = await asyncio.wait_for(self._probe_fn(self._info.host), timeout=10)
        except Exception:
            logger.debug("Reconnect failed for %s at %s", self._id, self._info.host, exc_info=True)
            return False
        if miner is None:
            return False
        miner_make = getattr(miner, "make", None) or ""
        miner_model = getattr(miner, "model", None) or ""
        # Resolve effective manufacturer: aftermarket firmware (BOS/VNish)
        # reports hardware make (e.g. "AntMiner") but we store the firmware
        # vendor (e.g. "Braiins") as manufacturer. Use the same resolution
        # so the identity check doesn't treat these as different devices.
        from pyasic_driver.capabilities import (
            FIRMWARE_MANUFACTURER,
            FW_STOCK,
            MAKE_TO_FAMILY,
            detect_firmware_variant,
        )
        family = MAKE_TO_FAMILY.get(miner_make, "")
        variant = detect_firmware_variant(miner, family) if family else FW_STOCK
        effective_make = FIRMWARE_MANUFACTURER.get(variant, miner_make)
        if (
            self._info.manufacturer
            and effective_make
            and effective_make != self._info.manufacturer
        ) or (
            self._info.model
            and miner_model
            and miner_model != self._info.model
        ):
            logger.warning(
                "Device identity mismatch at %s for %s: expected %s/%s, got %s/%s — "
                "IP may have been reassigned to a different device",
                self._info.host, self._id,
                self._info.manufacturer, self._info.model,
                miner_make, miner_model,
            )
            return False
        if self._secret is not None:
            from pyasic_driver.driver import _apply_credentials
            _apply_credentials(miner, self._secret)
        self._miner = miner
        from pyasic_driver.capabilities import build_capabilities
        self._caps = build_capabilities(miner)
        if self._on_caps_update and self._info.model:
            self._on_caps_update(self._info.model, self._caps)
        logger.info("Reconnected device %s at %s", self._id, self._info.host)
        return True

    async def _ensure_connected_or_raise(self) -> None:
        """Ensure connected, raising DeviceUnavailableError if not."""
        if not await self._ensure_connected():
            raise DeviceUnavailableError(device_id=self._id)

    def describe_device(self) -> tuple[driver_pb2.DeviceInfo, Capabilities]:
        return self._info, self._caps

    # --- Core: telemetry ---

    async def status(self) -> driver_pb2.DeviceMetrics:
        now = datetime.now(timezone.utc)
        if (
            self._last_status is not None
            and self._last_status_at is not None
            and (now - self._last_status_at).total_seconds() < self._cache_ttl_seconds
        ):
            return self._last_status

        if not await self._ensure_connected():
            raise DeviceUnavailableError(device_id=self._id)
        assert self._miner is not None

        try:
            data = await self._miner.get_data(exclude=["config"])
        except SDKError:
            raise
        except Exception:
            self._miner = None
            logger.warning("Failed to get data from %s", self._id, exc_info=True)
            metrics = driver_pb2.DeviceMetrics(
                device_id=self._id,
                health=driver_pb2.HEALTH_CRITICAL,
                health_reason="Failed to communicate with device",
            )
            metrics.timestamp.CopyFrom(_timestamp_from_datetime(now))
            return metrics

        metrics = self._convert_miner_data(data, now)
        self._last_status = metrics
        self._last_status_at = now
        return metrics

    def _convert_miner_data(self, data: Any, timestamp: datetime) -> driver_pb2.DeviceMetrics:
        if data is None:
            metrics = driver_pb2.DeviceMetrics(
                device_id=self._id,
                health=driver_pb2.HEALTH_UNKNOWN,
            )
            metrics.timestamp.CopyFrom(_timestamp_from_datetime(timestamp))
            return metrics

        health, health_reason = self._determine_health(data)

        hashrate_ths = getattr(data, "hashrate", None)
        wattage = getattr(data, "wattage", None)
        temp_avg = getattr(data, "temperature_avg", None)
        efficiency = getattr(data, "efficiency", None)

        metrics = driver_pb2.DeviceMetrics(
            device_id=self._id,
            health=health,
        )
        metrics.timestamp.CopyFrom(_timestamp_from_datetime(timestamp))

        if health_reason:
            metrics.health_reason = health_reason

        if _has_value(hashrate_ths):
            metrics.hashrate_hs.CopyFrom(_metric_rate(_to_float(hashrate_ths) * _THS_TO_HS))
        if _has_value(wattage):
            metrics.power_w.CopyFrom(_metric_gauge(_to_float(wattage)))
        if _has_value(temp_avg):
            metrics.temp_c.CopyFrom(_metric_gauge(_to_float(temp_avg)))
        if _has_value(efficiency) and _has_value(hashrate_ths):
            metrics.efficiency_jh.CopyFrom(_metric_gauge(_to_float(efficiency) * _JTH_TO_JH))

        for hb in self._convert_hashboards(data):
            metrics.hash_boards.append(hb)
        for fan in self._convert_fans(data):
            metrics.fan_metrics.append(fan)
        for psu in self._convert_psu(data):
            metrics.psu_metrics.append(psu)

        return metrics

    def _determine_health(self, data: Any) -> tuple[int, str | None]:
        is_mining = getattr(data, "is_mining", None)
        hashrate = getattr(data, "hashrate", None)
        errors = getattr(data, "errors", None)

        if is_mining is False:
            return driver_pb2.HEALTH_HEALTHY_INACTIVE, None
        if errors:
            return driver_pb2.HEALTH_WARNING, f"{len(errors)} error(s) reported"
        if is_mining and _has_value(hashrate):
            return driver_pb2.HEALTH_HEALTHY_ACTIVE, None
        if is_mining and not _has_value(hashrate):
            return driver_pb2.HEALTH_WARNING, "Mining but no hashrate detected"
        return driver_pb2.HEALTH_UNKNOWN, None

    def _convert_hashboards(self, data: Any) -> list[driver_pb2.HashBoardMetrics]:
        boards_raw = getattr(data, "hashboards", None)
        if not boards_raw:
            return []

        boards: list[driver_pb2.HashBoardMetrics] = []
        for i, board in enumerate(boards_raw):
            hashrate = getattr(board, "hashrate", None)
            temp = getattr(board, "temp", None)
            chips = getattr(board, "chips", None)
            chip_freq = getattr(board, "chip_freq", None)
            serial = getattr(board, "serial_number", None) or ""

            status = _determine_board_status(board)
            info = driver_pb2.ComponentInfo(index=i, name=f"hashboard_{i}", status=status)

            hb = driver_pb2.HashBoardMetrics(component_info=info)
            if serial:
                hb.serial_number = serial
            if _has_value(hashrate):
                hb.hash_rate_hs.CopyFrom(_metric_rate(_to_float(hashrate) * _THS_TO_HS))
            if _has_value(temp):
                hb.temp_c.CopyFrom(_metric_gauge(_to_float(temp)))
            if _has_value(chips) and chips is not None:
                hb.chip_count = int(chips)
            if _has_value(chip_freq):
                hb.chip_frequency_mhz.CopyFrom(_metric_gauge(_to_float(chip_freq)))

            boards.append(hb)
        return boards

    def _convert_fans(self, data: Any) -> list[driver_pb2.FanMetrics]:
        fans_raw = getattr(data, "fans", None)
        if not fans_raw:
            return []

        fans: list[driver_pb2.FanMetrics] = []
        for i, fan in enumerate(fans_raw):
            speed_raw = getattr(fan, "speed", None) if not isinstance(fan, (int, float)) else fan
            if not _has_value(speed_raw):
                continue
            speed = _to_float(speed_raw)

            status = (
                driver_pb2.COMPONENT_STATUS_HEALTHY
                if speed > 0
                else driver_pb2.COMPONENT_STATUS_OFFLINE
            )
            info = driver_pb2.ComponentInfo(index=i, name=f"fan_{i}", status=status)
            pb_fan = driver_pb2.FanMetrics(component_info=info)
            pb_fan.rpm.CopyFrom(_metric_gauge(speed))
            fans.append(pb_fan)

        return fans

    def _convert_psu(self, data: Any) -> list[driver_pb2.PSUMetrics]:
        wattage = getattr(data, "wattage", None)
        voltage = getattr(data, "voltage", None)
        current = getattr(data, "current", None)

        if not (_has_value(wattage) or _has_value(voltage) or _has_value(current)):
            return []

        status = (
            driver_pb2.COMPONENT_STATUS_HEALTHY
            if _has_value(wattage)
            else driver_pb2.COMPONENT_STATUS_UNKNOWN
        )
        info = driver_pb2.ComponentInfo(index=0, name="psu_0", status=status)

        psu = driver_pb2.PSUMetrics(component_info=info)
        if _has_value(wattage):
            psu.output_power_w.CopyFrom(_metric_gauge(_to_float(wattage)))
        if _has_value(voltage):
            psu.output_voltage_v.CopyFrom(_metric_gauge(_to_float(voltage)))
        if _has_value(current):
            psu.output_current_a.CopyFrom(_metric_gauge(_to_float(current)))

        return [psu]

    # --- Control ---

    # Benign APIError patterns from pyasic that indicate the device accepted
    # the command but didn't return a clean response.
    _BENIGN_API_ERROR_PATTERNS = (
        # Empty response body — pyasic fails to JSON-parse it
        "JSON decode error",
        # Device drops TCP connection mid-response during disruptive commands
        "HTTP error sending",
        # Response object partially initialized when connection drops
        "Attribute error sending",
        # Non-200 status after retries — device accepted command but returned error page
        "Failed to send command",
        # RPC socket closed before response on disruptive commands (reboot, power off)
        "No data was returned",
    )

    def _is_benign_command_error(self, command_name: str, exc: BaseException) -> bool:
        if command_name not in _DISRUPTIVE_CONTROL_COMMANDS:
            return False

        if isinstance(exc, APIError):
            msg = str(exc)
            return any(p in msg for p in self._BENIGN_API_ERROR_PATTERNS)

        return _is_timeout_exception(exc)

    async def _exec_pyasic_command(self, command_name: str, coro: Any) -> None:
        """Execute a pyasic command that returns bool, raising on failure.

        Privileged commands (reboot, stop/start mining) often produce broken
        responses because the device restarts mid-reply. Known-benign APIError
        patterns and transport timeouts for disruptive commands are treated as
        success; anything else propagates.
        """
        try:
            result = await coro
        except Exception as exc:
            if self._is_benign_command_error(command_name, exc):
                logger.info(
                    "Privileged command %s for %s returned benign %s, treating as success: %s",
                    command_name, self._id, exc.__class__.__name__, exc,
                )
                return
            if isinstance(exc, APIError):
                raise DeviceUnavailableError(device_id=self._id, cause=exc) from exc
            raise
        if result is False:
            raise DeviceCommandFailedError(
                command_name,
                self._id,
                miner_type=type(self._miner).__name__,
                miner_ip=getattr(self._miner, "ip", "?"),
            )

    async def start_mining(self) -> None:
        await self._ensure_connected_or_raise()
        assert self._miner is not None
        _require_cap(self._caps, "mining_start", self._id)
        await self._exec_pyasic_command("resume_mining", self._miner.resume_mining())

    async def stop_mining(self) -> None:
        await self._ensure_connected_or_raise()
        assert self._miner is not None
        _require_cap(self._caps, "mining_stop", self._id)
        await self._exec_pyasic_command("stop_mining", self._miner.stop_mining())

    async def reboot(self) -> None:
        await self._ensure_connected_or_raise()
        assert self._miner is not None
        _require_cap(self._caps, "reboot", self._id)
        await self._exec_pyasic_command("reboot", self._miner.reboot())

    async def blink_led(self) -> None:
        await self._ensure_connected_or_raise()
        assert self._miner is not None
        _require_cap(self._caps, "led_blink", self._id)
        await self._exec_pyasic_command("fault_light_on", self._miner.fault_light_on())

        async def _turn_off_led() -> None:
            try:
                if self._miner is not None:
                    await self._miner.fault_light_off()
            except Exception:
                logger.warning("Failed to turn off LED for %s", self._id, exc_info=True)

        asyncio.get_running_loop().call_later(
            _BLINK_LED_DURATION_SECS,
            lambda: asyncio.ensure_future(_turn_off_led()),
        )

    # --- Configuration ---

    async def get_mining_pools(self) -> list[driver_pb2.ConfiguredPool]:
        await self._ensure_connected_or_raise()
        assert self._miner is not None
        _require_cap(self._caps, "get_mining_pools", self._id)
        config = await self._miner.get_config()
        if config is None:
            return []

        pools: list[driver_pb2.ConfiguredPool] = []
        pool_groups = getattr(config, "pools", None)
        if pool_groups:
            groups = getattr(pool_groups, "groups", None) or []
            for i, group in enumerate(groups):
                group_pools = getattr(group, "pools", None) or []
                for pool in group_pools:
                    url = getattr(pool, "url", "") or ""
                    user = getattr(pool, "user", "") or ""
                    if url:
                        pools.append(driver_pb2.ConfiguredPool(priority=i, url=url, username=user))
        return pools

    async def update_mining_pools(self, pools: list[driver_pb2.MiningPool]) -> None:
        await self._ensure_connected_or_raise()
        assert self._miner is not None
        _require_cap(self._caps, "update_mining_pools", self._id)

        from pyasic.config import MinerConfig as PyasicMinerConfig
        from pyasic.config.pools import Pool, PoolConfig, PoolGroup

        config = await self._miner.get_config()
        if config is None:
            config = PyasicMinerConfig()

        all_pools = [
            Pool(url=p.url, user=p.worker_name, password=_DEFAULT_STRATUM_PASSWORD)
            for p in sorted(pools, key=lambda p: p.priority)
        ]
        new_pool_config = PoolConfig(groups=[PoolGroup(pools=all_pools, quota=1)])
        config = PyasicMinerConfig.from_dict(config.as_dict() | {"pools": new_pool_config.as_dict()})

        logger.info("Updating pools for %s: %d pool(s)", self._id, len(pools))
        await self._miner.send_config(config)

    async def set_power_target(self, performance_mode: int) -> None:
        await self._ensure_connected_or_raise()
        assert self._miner is not None
        _require_cap(self._caps, "power_mode_efficiency", self._id)

        if getattr(self._miner, "supports_presets", False):
            await self._set_power_target_preset(performance_mode)
        else:
            await self._set_power_target_mode(performance_mode)

    async def _set_power_target_preset(self, performance_mode: int) -> None:
        """Set power target on preset-based miners by selecting the appropriate preset."""
        assert self._miner is not None

        from pyasic.errors import APIError

        try:
            raw = await self._miner.web.autotune_presets()
        except APIError:
            raise DeviceCommandFailedError(
                "set_power_target", self._id,
                miner_type=type(self._miner).__name__,
                miner_ip=getattr(self._miner, "ip", "?"),
            )

        presets = raw if isinstance(raw, list) else raw.get("presets", []) if isinstance(raw, dict) else []

        tuned_powers: list[int] = []
        for p in presets:
            if p.get("status") != "tuned":
                continue
            try:
                pw = int(p["pretty"].split("~")[0].replace("watt", "").strip())
            except (KeyError, ValueError, IndexError):
                continue
            tuned_powers.append(pw)

        if not tuned_powers:
            raise UnsupportedCapabilityError(
                "set_power_target (no tuned presets — run autotuning first)",
                device_id=self._id,
            )

        if performance_mode == driver_pb2.PERFORMANCE_MODE_MAXIMUM_HASHRATE:
            target_wattage = max(tuned_powers)
        elif performance_mode == driver_pb2.PERFORMANCE_MODE_EFFICIENCY:
            target_wattage = min(tuned_powers)
        else:
            sorted_powers = sorted(tuned_powers)
            target_wattage = sorted_powers[len(sorted_powers) // 2]

        logger.info(
            "Setting preset power limit to %dW for %s (mode=%s)",
            target_wattage, self._id, performance_mode,
        )
        await self._exec_pyasic_command(
            "set_power_target", self._miner.set_power_limit(target_wattage),
        )

    async def _set_power_target_mode(self, performance_mode: int) -> None:
        """Set power target on mode-based miners using HPM/LPM/Normal."""
        assert self._miner is not None

        from pyasic.config import MinerConfig as PyasicMinerConfig
        from pyasic.config.mining import MiningModeHPM, MiningModeLPM, MiningModeNormal

        mode_map = {
            driver_pb2.PERFORMANCE_MODE_MAXIMUM_HASHRATE: MiningModeHPM(),
            driver_pb2.PERFORMANCE_MODE_EFFICIENCY: MiningModeLPM(),
        }
        mining_mode = mode_map.get(performance_mode)
        if mining_mode is None:
            logger.warning(
                "Unrecognized performance mode %s for %s, using normal", performance_mode, self._id,
            )
            mining_mode = MiningModeNormal()

        config = await self._miner.get_config()
        if config is None:
            config = PyasicMinerConfig()

        config_dict = config.as_dict()
        config_dict["mining_mode"] = {"mode": mining_mode.mode}
        new_config = PyasicMinerConfig.from_dict(config_dict)

        await self._miner.send_config(new_config)

    # --- Maintenance ---

    async def firmware_update(self) -> None:
        await self._ensure_connected_or_raise()
        assert self._miner is not None
        _require_cap(self._caps, "firmware", self._id)
        await self._exec_pyasic_command("upgrade_firmware", self._miner.upgrade_firmware())

    # --- Error reporting ---

    async def get_errors(self) -> driver_pb2.DeviceErrors:
        now = datetime.now(timezone.utc)
        if not await self._ensure_connected():
            return driver_pb2.DeviceErrors(device_id=self._id)
        assert self._miner is not None
        try:
            raw_errors = await self._miner.get_errors()
        except Exception:
            self._miner = None
            logger.warning("Failed to get errors from %s", self._id, exc_info=True)
            return driver_pb2.DeviceErrors(device_id=self._id)

        if not raw_errors:
            return driver_pb2.DeviceErrors(device_id=self._id)

        pb_errors = driver_pb2.DeviceErrors(device_id=self._id)
        now_ts = _timestamp_from_datetime(now)

        for raw_err in raw_errors:
            error_msg = getattr(raw_err, "error_message", None) or str(raw_err)
            error_code = getattr(raw_err, "error_code", None)
            severity = _infer_severity(error_msg)
            component = _infer_component(error_msg)
            miner_error = _infer_miner_error(error_msg, error_code)

            pb_error = driver_pb2.DeviceError(
                miner_error=miner_error,
                cause_summary=error_msg,
                recommended_action="Check device status",
                severity=severity,
                device_id=self._id,
                summary=error_msg,
                component_type=component,
            )
            pb_error.first_seen_at.CopyFrom(now_ts)
            pb_error.last_seen_at.CopyFrom(now_ts)

            if error_code is not None:
                pb_error.vendor_attributes["vendor_error_code"] = str(error_code)

            pb_errors.errors.append(pb_error)

        return pb_errors

    def close(self) -> None:
        self._miner = None
        self._last_status = None
        self._last_status_at = None
