"""Component-level telemetry metrics.

This module defines all hardware component metric types: hash boards, ASICs, PSUs,
fans, control boards, and sensors.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime

from proto_fleet_sdk.enums import ComponentStatus
from proto_fleet_sdk.telemetry.metrics import MetricValue

__all__ = [
    "ComponentInfo",
    "HashBoardMetrics",
    "ASICMetrics",
    "PSUMetrics",
    "FanMetrics",
    "ControlBoardMetrics",
    "SensorMetrics",
]


@dataclass(frozen=True)
class ComponentInfo:
    """Common metadata for all hardware components."""

    index: int
    name: str
    status: ComponentStatus
    status_reason: str | None = None
    timestamp: datetime | None = None


@dataclass(frozen=True)
class ASICMetrics:
    """Telemetry from an individual ASIC chip.

    ASICs are sub-components of hash boards and can have independent health status.
    """

    component_info: ComponentInfo
    temp_c: MetricValue | None = None
    frequency_mhz: MetricValue | None = None
    voltage_v: MetricValue | None = None
    hashrate_hs: MetricValue | None = None


@dataclass(frozen=True)
class FanMetrics:
    """Telemetry from a cooling fan.

    Fans can report speed in both RPM (absolute) and percent (relative to max).
    """

    component_info: ComponentInfo
    rpm: MetricValue | None = None
    temp_c: MetricValue | None = None
    percent: MetricValue | None = None


@dataclass(frozen=True)
class HashBoardMetrics:
    """Telemetry from an ASIC hashboard.

    Hash boards are the primary computing components containing ASIC chips.
    """

    component_info: ComponentInfo
    serial_number: str | None = None
    hash_rate_hs: MetricValue | None = None
    temp_c: MetricValue | None = None
    voltage_v: MetricValue | None = None
    current_a: MetricValue | None = None
    inlet_temp_c: MetricValue | None = None
    outlet_temp_c: MetricValue | None = None
    ambient_temp_c: MetricValue | None = None
    chip_count: int | None = None
    chip_frequency_mhz: MetricValue | None = None
    asics: list[ASICMetrics] = field(default_factory=list)
    fan_metrics: list[FanMetrics] = field(default_factory=list)


@dataclass(frozen=True)
class PSUMetrics:
    """Telemetry from a power supply unit.

    PSUs can report both input (from wall) and output (to device) measurements.
    """

    component_info: ComponentInfo
    output_power_w: MetricValue | None = None
    output_voltage_v: MetricValue | None = None
    output_current_a: MetricValue | None = None
    input_power_w: MetricValue | None = None
    input_voltage_v: MetricValue | None = None
    input_current_a: MetricValue | None = None
    hotspot_temp_c: MetricValue | None = None
    efficiency_percent: MetricValue | None = None
    fan_metrics: list[FanMetrics] = field(default_factory=list)


@dataclass(frozen=True)
class ControlBoardMetrics:
    """Telemetry from the device control board."""

    component_info: ComponentInfo


@dataclass(frozen=True)
class SensorMetrics:
    """Miscellaneous sensor metrics.

    For sensors that don't fit into other specific component categories.
    Examples: ambient temperature, humidity, vibration sensors, etc.
    """

    component_info: ComponentInfo
    type: str | None = None
    unit: str | None = None
    value: MetricValue | None = None
