"""Enumerations for Proto Fleet SDK.

This module defines all enum types used throughout the SDK, matching the protobuf definitions
in driver.proto for seamless conversion.
"""

from __future__ import annotations

from enum import IntEnum

__all__ = [
    "HealthStatus",
    "ComponentStatus",
    "MetricKind",
    "CoolingMode",
    "PerformanceMode",
]


class HealthStatus(IntEnum):
    """Overall health state of a mining device.

    Status progression typically follows:
    HEALTH_UNKNOWN → HEALTH_HEALTHY_* → HEALTH_WARNING → HEALTH_CRITICAL
    """

    HEALTH_STATUS_UNSPECIFIED = 0
    HEALTH_UNKNOWN = 1  # Unknown health state (e.g., device unreachable)
    HEALTH_HEALTHY_ACTIVE = 2  # Mining and all systems healthy
    HEALTH_HEALTHY_INACTIVE = 3  # All systems healthy but not actively mining
    HEALTH_WARNING = 4  # Degraded performance but still operational
    HEALTH_CRITICAL = 5  # Failed, non-functional, or requires immediate attention
    HEALTH_NEEDS_MINING_POOL = 6  # Online but needs mining pool configured

    @classmethod
    def _missing_(cls, value: object) -> HealthStatus | None:
        """Map unknown proto values to HEALTH_UNKNOWN for forward compatibility."""
        if isinstance(value, int):
            return cls.HEALTH_UNKNOWN
        return None


class ComponentStatus(IntEnum):
    """Health and operational state of an individual component.

    Typical status progression:
    COMPONENT_STATUS_UNKNOWN → COMPONENT_STATUS_HEALTHY → COMPONENT_STATUS_WARNING →
    COMPONENT_STATUS_CRITICAL/COMPONENT_STATUS_OFFLINE
    """

    COMPONENT_STATUS_UNSPECIFIED = 0
    COMPONENT_STATUS_UNKNOWN = 1  # Unknown status (e.g., no telemetry data)
    COMPONENT_STATUS_HEALTHY = 2  # Operating normally within acceptable parameters
    COMPONENT_STATUS_WARNING = 3  # Degraded performance but still functional
    COMPONENT_STATUS_CRITICAL = 4  # Failed, malfunctioning, or out of safe operating range
    COMPONENT_STATUS_OFFLINE = 5  # Not responding or unreachable
    COMPONENT_STATUS_DISABLED = 6  # Intentionally disabled by operator or firmware

    @classmethod
    def _missing_(cls, value: object) -> ComponentStatus | None:
        if isinstance(value, int):
            return cls.COMPONENT_STATUS_UNKNOWN
        return None


class MetricKind(IntEnum):
    """Type of metric value and how it should be interpreted.

    This classification affects how metrics are aggregated, displayed, and stored.
    """

    METRIC_KIND_UNSPECIFIED = 0
    METRIC_KIND_GAUGE = 1  # Point-in-time measurement (e.g., temperature, RPM)
    METRIC_KIND_RATE = 2  # Rate of change per second (e.g., hashrate as H/s)
    METRIC_KIND_COUNTER = 3  # Monotonically increasing counter (e.g., shares accepted, uptime)

    @classmethod
    def _missing_(cls, value: object) -> MetricKind | None:
        if isinstance(value, int):
            return cls.METRIC_KIND_UNSPECIFIED
        return None



class CoolingMode(IntEnum):
    """Cooling mode for mining devices."""

    COOLING_MODE_UNSPECIFIED = 0
    COOLING_MODE_AIR_COOLED = 1
    COOLING_MODE_IMMERSION_COOLED = 2
    COOLING_MODE_MANUAL = 3  # User sets fan speed manually

    @classmethod
    def _missing_(cls, value: object) -> CoolingMode | None:
        if isinstance(value, int):
            return cls.COOLING_MODE_UNSPECIFIED
        return None


class PerformanceMode(IntEnum):
    """Performance mode for power target settings."""

    PERFORMANCE_MODE_UNSPECIFIED = 0
    PERFORMANCE_MODE_MAXIMUM_HASHRATE = 1
    PERFORMANCE_MODE_EFFICIENCY = 2

    @classmethod
    def _missing_(cls, value: object) -> PerformanceMode | None:
        if isinstance(value, int):
            return cls.PERFORMANCE_MODE_UNSPECIFIED
        return None
