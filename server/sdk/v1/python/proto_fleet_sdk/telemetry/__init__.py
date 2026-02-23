"""Telemetry model for Proto Fleet SDK.

This module provides the complete telemetry V2 model with hierarchical component metrics,
statistical metadata, and type-safe metric values.
"""

from proto_fleet_sdk.telemetry.components import (
    ASICMetrics,
    ComponentInfo,
    ControlBoardMetrics,
    FanMetrics,
    HashBoardMetrics,
    PSUMetrics,
    SensorMetrics,
)
from proto_fleet_sdk.telemetry.converters import (
    hs_to_ths,
    jh_to_jth,
    jth_to_jh,
    ths_to_hs,
)
from proto_fleet_sdk.telemetry.metrics import DeviceMetrics, MetricValue, MetricValueMetaData

__all__ = [
    # Metrics
    "MetricValue",
    "MetricValueMetaData",
    "DeviceMetrics",
    # Components
    "ComponentInfo",
    "HashBoardMetrics",
    "ASICMetrics",
    "PSUMetrics",
    "FanMetrics",
    "ControlBoardMetrics",
    "SensorMetrics",
    # Converters
    "ths_to_hs",
    "jth_to_jh",
    "hs_to_ths",
    "jh_to_jth",
]
