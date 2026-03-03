"""Validation utilities for SDK types.

This module provides validation functions for common SDK data structures.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from proto_fleet_sdk.errors import InvalidConfigError
from proto_fleet_sdk.telemetry.metrics import MetricValue

if TYPE_CHECKING:
    from proto_fleet_sdk.telemetry import DeviceMetrics
    from proto_fleet_sdk.types import Capabilities

__all__ = ["validate_device_metrics", "validate_capabilities"]

MIN_TEMP_C = -50
MAX_TEMP_C = 150


def _validate_non_negative(metric: MetricValue | None, field_name: str) -> None:
    if metric is not None and metric.value < 0:
        raise InvalidConfigError(f"{field_name} cannot be negative: {metric.value}")


def _validate_temp_range(metric: MetricValue | None, field_name: str) -> None:
    if metric is not None and (metric.value < MIN_TEMP_C or metric.value > MAX_TEMP_C):
        raise InvalidConfigError(f"{field_name} out of reasonable range: {metric.value}")


def validate_device_metrics(metrics: DeviceMetrics) -> None:
    """Validate DeviceMetrics structure and values.

    Ensures metrics meet basic quality requirements before being sent
    to the Fleet server. Catches common errors like negative values,
    invalid enums, or malformed component data.
    """
    if metrics is None:
        raise InvalidConfigError("metrics cannot be None")

    if not metrics.device_id:
        raise InvalidConfigError("device_id cannot be empty")

    if metrics.timestamp is None:
        raise InvalidConfigError("timestamp cannot be None")

    _validate_non_negative(metrics.hashrate_hs, "hashrate_hs")
    _validate_non_negative(metrics.power_w, "power_w")
    _validate_non_negative(metrics.efficiency_jh, "efficiency_jh")
    _validate_temp_range(metrics.temp_c, "temp_c")

    for idx, board in enumerate(metrics.hash_boards):
        if board.component_info.index < 0:
            raise InvalidConfigError(
                f"hash_boards[{idx}].component_info.index cannot be negative: {board.component_info.index}"
            )
        _validate_non_negative(board.hash_rate_hs, f"hash_boards[{idx}].hash_rate_hs")
        _validate_temp_range(board.temp_c, f"hash_boards[{idx}].temp_c")

    for idx, psu in enumerate(metrics.psu_metrics):
        if psu.component_info.index < 0:
            raise InvalidConfigError(
                f"psu_metrics[{idx}].component_info.index cannot be negative: {psu.component_info.index}"
            )
        _validate_non_negative(psu.output_power_w, f"psu_metrics[{idx}].output_power_w")

    for idx, fan in enumerate(metrics.fan_metrics):
        if fan.component_info.index < 0:
            raise InvalidConfigError(
                f"fan_metrics[{idx}].component_info.index cannot be negative: {fan.component_info.index}"
            )
        _validate_non_negative(fan.rpm, f"fan_metrics[{idx}].rpm")


def validate_capabilities(capabilities: Capabilities) -> None:
    """Validate Capabilities structure."""
    if not isinstance(capabilities, dict):
        raise InvalidConfigError("capabilities must be a dictionary")

    for key, value in capabilities.items():
        if not isinstance(key, str):
            raise InvalidConfigError(f"capability keys must be strings, got {type(key).__name__}")
        if not isinstance(value, bool):
            raise InvalidConfigError(
                f"capability values must be boolean, got {type(value).__name__} for key '{key}'"
            )
