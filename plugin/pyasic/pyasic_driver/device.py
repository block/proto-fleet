"""Generic PyASIC device implementation.

Single device class for ALL manufacturers supported by pyasic. Delegates all operations
to pyasic's unified API. No manufacturer-specific subclasses needed.
"""

from __future__ import annotations

import logging
from datetime import datetime, timezone
from typing import Any

import grpc
from proto_fleet_sdk.enums import (
    ComponentStatus,
    CoolingMode,
    HealthStatus,
    MetricKind,
    PerformanceMode,
)
from proto_fleet_sdk.error_codes import (
    ComponentType,
    DeviceError,
    DeviceErrors,
    MinerError,
    Severity,
)
from proto_fleet_sdk.errors import UnsupportedCapabilityError
from proto_fleet_sdk.telemetry import DeviceMetrics, jth_to_jh, ths_to_hs
from proto_fleet_sdk.telemetry.components import (
    ComponentInfo,
    FanMetrics,
    HashBoardMetrics,
    PSUMetrics,
)
from proto_fleet_sdk.telemetry.metrics import MetricValue
from proto_fleet_sdk.types import Capabilities, ConfiguredPool, DeviceInfo, MiningPoolConfig

logger = logging.getLogger(__name__)


def _metric_gauge(value: float) -> MetricValue:
    return MetricValue(value=value, kind=MetricKind.METRIC_KIND_GAUGE)


def _metric_rate(value: float) -> MetricValue:
    return MetricValue(value=value, kind=MetricKind.METRIC_KIND_RATE)


def _has_value(val: object) -> bool:
    return val is not None and val != 0


def _to_float(val: object) -> float:
    """Convert a value known to be non-None to float."""
    return float(val)  # type: ignore[arg-type]


def _require_cap(caps: Capabilities, cap: str, device_id: str) -> None:
    if not caps.get(cap):
        raise UnsupportedCapabilityError(cap, device_id=device_id)


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


def _determine_board_status(board: Any) -> ComponentStatus:
    hashrate = getattr(board, "hashrate", None)
    temp = getattr(board, "temp", None)
    chips = getattr(board, "chips", None)
    expected = getattr(board, "expected_chips", None)

    if hashrate and hashrate > 0:
        if expected and chips and chips < expected:
            return ComponentStatus.COMPONENT_STATUS_WARNING
        return ComponentStatus.COMPONENT_STATUS_HEALTHY
    if temp and temp > 0:
        return ComponentStatus.COMPONENT_STATUS_WARNING
    return ComponentStatus.COMPONENT_STATUS_OFFLINE


class PyAsicDevice:
    """Device wrapping a pyasic miner instance.

    Handles telemetry, control, pool configuration, and error reporting for any
    manufacturer that pyasic supports, using pyasic's unified async API.
    """

    def __init__(
        self,
        device_id: str,
        miner: Any,
        device_info: DeviceInfo,
        caps: Capabilities,
        cache_ttl_seconds: int = 5,
    ) -> None:
        self._id = device_id
        self._miner = miner
        self._info = device_info
        self._caps = caps
        self._cache_ttl_seconds = cache_ttl_seconds
        self._last_status: DeviceMetrics | None = None
        self._last_status_at: datetime | None = None

    def id(self) -> str:
        return self._id

    async def describe_device(self, ctx: grpc.ServicerContext) -> tuple[DeviceInfo, Capabilities]:
        return self._info, self._caps

    # --- Core: telemetry ---

    async def status(self, ctx: grpc.ServicerContext) -> DeviceMetrics:
        now = datetime.now(timezone.utc)
        if (
            self._last_status is not None
            and self._last_status_at is not None
            and (now - self._last_status_at).total_seconds() < self._cache_ttl_seconds
        ):
            return self._last_status

        try:
            data = await self._miner.get_data()
        except Exception:
            logger.warning("Failed to get data from %s", self._id, exc_info=True)
            return DeviceMetrics(
                device_id=self._id,
                timestamp=now,
                health=HealthStatus.HEALTH_CRITICAL,
                health_reason="Failed to communicate with device",
            )

        metrics = self._convert_miner_data(data, now)
        self._last_status = metrics
        self._last_status_at = now
        return metrics

    def _convert_miner_data(self, data: Any, timestamp: datetime) -> DeviceMetrics:
        if data is None:
            return DeviceMetrics(
                device_id=self._id,
                timestamp=timestamp,
                health=HealthStatus.HEALTH_UNKNOWN,
            )

        health, health_reason = self._determine_health(data)

        hashrate_ths = getattr(data, "hashrate", None)
        wattage = getattr(data, "wattage", None)
        temp_avg = getattr(data, "temperature_avg", None)
        efficiency = getattr(data, "efficiency", None)

        hashrate_hs = _metric_rate(ths_to_hs(hashrate_ths)) if _has_value(hashrate_ths) else None
        power_w = _metric_gauge(_to_float(wattage)) if _has_value(wattage) else None
        temp_c = _metric_gauge(_to_float(temp_avg)) if _has_value(temp_avg) else None
        efficiency_jh = _metric_gauge(jth_to_jh(efficiency)) if _has_value(efficiency) else None

        hash_boards = self._convert_hashboards(data)
        fan_metrics = self._convert_fans(data)
        psu_metrics = self._convert_psu(data)

        return DeviceMetrics(
            device_id=self._id,
            timestamp=timestamp,
            health=health,
            health_reason=health_reason,
            hashrate_hs=hashrate_hs,
            power_w=power_w,
            temp_c=temp_c,
            efficiency_jh=efficiency_jh,
            hash_boards=hash_boards,
            fan_metrics=fan_metrics,
            psu_metrics=psu_metrics,
        )

    def _determine_health(self, data: Any) -> tuple[HealthStatus, str | None]:
        is_mining = getattr(data, "is_mining", None)
        hashrate = getattr(data, "hashrate", None)
        errors = getattr(data, "errors", None)

        if errors and len(errors) > 0:
            return HealthStatus.HEALTH_WARNING, f"{len(errors)} error(s) reported"
        if is_mining and _has_value(hashrate):
            return HealthStatus.HEALTH_HEALTHY_ACTIVE, None
        if is_mining and not _has_value(hashrate):
            return HealthStatus.HEALTH_WARNING, "Mining but no hashrate detected"
        if is_mining is False:
            return HealthStatus.HEALTH_HEALTHY_INACTIVE, None
        return HealthStatus.HEALTH_UNKNOWN, None

    def _convert_hashboards(self, data: Any) -> list[HashBoardMetrics]:
        boards_raw = getattr(data, "hashboards", None)
        if not boards_raw:
            return []

        boards: list[HashBoardMetrics] = []
        for i, board in enumerate(boards_raw):
            hashrate = getattr(board, "hashrate", None)
            temp = getattr(board, "temp", None)
            chips = getattr(board, "chips", None)
            chip_freq = getattr(board, "chip_freq", None)
            serial = getattr(board, "serial_number", None) or ""

            status = _determine_board_status(board)
            info = ComponentInfo(index=i, name=f"hashboard_{i}", status=status)

            boards.append(
                HashBoardMetrics(
                    component_info=info,
                    serial_number=serial,
                    hash_rate_hs=_metric_rate(ths_to_hs(hashrate)) if _has_value(hashrate) else None,
                    temp_c=_metric_gauge(_to_float(temp)) if _has_value(temp) else None,
                    chip_count=chips if _has_value(chips) else None,
                    chip_frequency_mhz=_metric_gauge(_to_float(chip_freq)) if _has_value(chip_freq) else None,
                )
            )
        return boards

    def _convert_fans(self, data: Any) -> list[FanMetrics]:
        fans_raw = getattr(data, "fans", None)
        if not fans_raw:
            return []

        fans: list[FanMetrics] = []
        for i, fan in enumerate(fans_raw):
            speed_raw = getattr(fan, "speed", None) if not isinstance(fan, (int, float)) else fan
            if not _has_value(speed_raw):
                continue
            speed = _to_float(speed_raw)

            status = (
                ComponentStatus.COMPONENT_STATUS_HEALTHY
                if speed > 0
                else ComponentStatus.COMPONENT_STATUS_OFFLINE
            )
            info = ComponentInfo(index=i, name=f"fan_{i}", status=status)
            fans.append(FanMetrics(component_info=info, rpm=_metric_gauge(speed)))

        return fans

    def _convert_psu(self, data: Any) -> list[PSUMetrics]:
        wattage = getattr(data, "wattage", None)
        voltage = getattr(data, "voltage", None)
        current = getattr(data, "current", None)

        if not (_has_value(wattage) or _has_value(voltage) or _has_value(current)):
            return []

        status = (
            ComponentStatus.COMPONENT_STATUS_HEALTHY
            if _has_value(wattage)
            else ComponentStatus.COMPONENT_STATUS_UNKNOWN
        )
        info = ComponentInfo(index=0, name="psu_0", status=status)

        return [
            PSUMetrics(
                component_info=info,
                output_power_w=_metric_gauge(_to_float(wattage)) if _has_value(wattage) else None,
                output_voltage_v=_metric_gauge(_to_float(voltage)) if _has_value(voltage) else None,
                output_current_a=_metric_gauge(_to_float(current)) if _has_value(current) else None,
            )
        ]

    # --- Control ---

    async def start_mining(self, ctx: grpc.ServicerContext) -> None:
        _require_cap(self._caps, "mining_start", self._id)
        await self._miner.resume_mining()

    async def stop_mining(self, ctx: grpc.ServicerContext) -> None:
        _require_cap(self._caps, "mining_stop", self._id)
        await self._miner.stop_mining()

    async def reboot(self, ctx: grpc.ServicerContext) -> None:
        _require_cap(self._caps, "reboot", self._id)
        await self._miner.reboot()

    async def blink_led(self, ctx: grpc.ServicerContext) -> None:
        _require_cap(self._caps, "led_blink", self._id)
        await self._miner.fault_light_on()

    # --- Configuration ---

    async def get_mining_pools(self, ctx: grpc.ServicerContext) -> list[ConfiguredPool]:
        _require_cap(self._caps, "get_mining_pools", self._id)
        config = await self._miner.get_config()
        if config is None:
            return []

        pools: list[ConfiguredPool] = []
        pool_groups = getattr(config, "pools", None)
        if pool_groups:
            groups = getattr(pool_groups, "groups", None) or []
            for i, group in enumerate(groups):
                group_pools = getattr(group, "pools", None) or []
                for pool in group_pools:
                    url = getattr(pool, "url", "") or ""
                    user = getattr(pool, "user", "") or ""
                    if url:
                        pools.append(ConfiguredPool(priority=i, url=url, username=user))
        return pools

    async def update_mining_pools(self, ctx: grpc.ServicerContext, pools: list[MiningPoolConfig]) -> None:
        _require_cap(self._caps, "update_mining_pools", self._id)

        from pyasic.config import MinerConfig as PyasicMinerConfig

        config = await self._miner.get_config()
        if config is None:
            config = PyasicMinerConfig()

        pool_groups = getattr(config, "pools", None)
        if pool_groups is None:
            logger.warning("Cannot update pools for %s: no pool config structure", self._id)
            return

        # Build pyasic pool group from our SDK pool configs
        from pyasic.config.pools import Pool, PoolConfig, PoolGroup

        groups: list[PoolGroup] = []
        for pool_cfg in sorted(pools, key=lambda p: p.priority):
            group = PoolGroup(
                pools=[Pool(url=pool_cfg.url, user=pool_cfg.worker_name, password="")],
                quota=1,
            )
            groups.append(group)

        new_pool_config = PoolConfig(groups=groups)
        config = PyasicMinerConfig.from_dict(config.as_dict() | {"pools": new_pool_config.as_dict()})
        await self._miner.send_config(config)

    async def set_cooling_mode(self, ctx: grpc.ServicerContext, mode: CoolingMode) -> None:
        raise UnsupportedCapabilityError("set_cooling_mode", device_id=self._id)

    async def get_cooling_mode(self, ctx: grpc.ServicerContext) -> CoolingMode:
        raise UnsupportedCapabilityError("get_cooling_mode", device_id=self._id)

    async def set_power_target(self, ctx: grpc.ServicerContext, performance_mode: PerformanceMode) -> None:
        raise UnsupportedCapabilityError("set_power_target", device_id=self._id)

    async def update_miner_password(
        self, ctx: grpc.ServicerContext, current_password: str, new_password: str
    ) -> None:
        raise UnsupportedCapabilityError("update_miner_password", device_id=self._id)

    # --- Maintenance ---

    async def download_logs(
        self, ctx: grpc.ServicerContext, since: datetime | None = None, batch_log_uuid: str | None = None
    ) -> tuple[str, bool]:
        raise UnsupportedCapabilityError("download_logs", device_id=self._id)

    async def firmware_update(self, ctx: grpc.ServicerContext) -> None:
        _require_cap(self._caps, "firmware", self._id)
        await self._miner.upgrade_firmware()

    async def unpair(self, ctx: grpc.ServicerContext) -> None:
        pass

    # --- Error reporting ---

    async def get_errors(self, ctx: grpc.ServicerContext) -> DeviceErrors:
        now = datetime.now(timezone.utc)
        try:
            raw_errors = await self._miner.get_errors()
        except Exception:
            logger.warning("Failed to get errors from %s", self._id, exc_info=True)
            return DeviceErrors(device_id=self._id, errors=())

        if not raw_errors:
            return DeviceErrors(device_id=self._id, errors=())

        errors: list[DeviceError] = []
        for raw_err in raw_errors:
            error_msg = getattr(raw_err, "error_message", None) or str(raw_err)
            error_code = getattr(raw_err, "error_code", None)
            severity = _infer_severity(error_msg)
            component = _infer_component(error_msg)

            vendor_attrs: dict[str, str] = {}
            if error_code is not None:
                vendor_attrs["vendor_error_code"] = str(error_code)

            errors.append(
                DeviceError(
                    miner_error=MinerError.VENDOR_ERROR_UNMAPPED,
                    cause_summary=error_msg,
                    recommended_action="Check device status",
                    severity=severity,
                    first_seen_at=now,
                    last_seen_at=now,
                    device_id=self._id,
                    summary=error_msg,
                    component_type=component,
                    vendor_attributes=vendor_attrs,
                )
            )

        return DeviceErrors(device_id=self._id, errors=tuple(errors))

    async def close(self, ctx: grpc.ServicerContext) -> None:
        self._last_status = None
        self._last_status_at = None
