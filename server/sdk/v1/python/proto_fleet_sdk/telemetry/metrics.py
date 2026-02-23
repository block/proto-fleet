"""Core telemetry metrics types.

This module defines the top-level telemetry types including MetricValue with metadata
and the DeviceMetrics container for complete device telemetry snapshots.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timedelta
from typing import TYPE_CHECKING

from proto_fleet_sdk.enums import HealthStatus, MetricKind
from proto_fleet_sdk.errors import InvalidConfigError

if TYPE_CHECKING:
    from proto_fleet_sdk.telemetry.components import (
        ControlBoardMetrics,
        FanMetrics,
        HashBoardMetrics,
        PSUMetrics,
        SensorMetrics,
    )

__all__ = ["MetricValue", "MetricValueMetaData", "DeviceMetrics"]


@dataclass(frozen=True)
class MetricValueMetaData:
    """Statistical metadata for a metric value.

    Provides aggregated statistics over a time window for metrics that are sampled
    or aggregated over time.
    """

    window: timedelta | None = None
    min: float | None = None
    max: float | None = None
    avg: float | None = None
    std_dev: float | None = None
    timestamp: datetime | None = None


@dataclass(frozen=True)
class MetricValue:
    """A single telemetry measurement with optional statistical metadata."""

    value: float
    kind: MetricKind = MetricKind.METRIC_KIND_GAUGE
    metadata: MetricValueMetaData | None = None

    def __post_init__(self) -> None:
        if not isinstance(self.value, (int, float)):
            raise InvalidConfigError(
                f"MetricValue.value must be numeric, got {type(self.value)}"
            )


@dataclass(frozen=True)
class DeviceMetrics:
    """Complete telemetry snapshot for a mining device.

    This is the top-level telemetry container that includes device-level health,
    aggregated metrics, and detailed component-level metrics.
    """

    device_id: str
    timestamp: datetime
    health: HealthStatus
    health_reason: str | None = None
    hashrate_hs: MetricValue | None = None
    temp_c: MetricValue | None = None
    fan_rpm: MetricValue | None = None
    power_w: MetricValue | None = None
    efficiency_jh: MetricValue | None = None
    hash_boards: list[HashBoardMetrics] = field(default_factory=list)
    psu_metrics: list[PSUMetrics] = field(default_factory=list)
    control_board_metrics: list[ControlBoardMetrics] = field(default_factory=list)
    fan_metrics: list[FanMetrics] = field(default_factory=list)
    sensor_metrics: list[SensorMetrics] = field(default_factory=list)
