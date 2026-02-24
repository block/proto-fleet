"""Tests for validation utilities."""

from __future__ import annotations

from datetime import datetime, timezone

import pytest

from proto_fleet_sdk.enums import ComponentStatus, HealthStatus

# Alias for readability — HealthStatus uses proto naming convention
HEALTH_OK = HealthStatus.HEALTH_HEALTHY_ACTIVE
from proto_fleet_sdk.errors import InvalidConfigError
from proto_fleet_sdk.telemetry import DeviceMetrics
from proto_fleet_sdk.telemetry.components import ComponentInfo, FanMetrics, HashBoardMetrics, PSUMetrics
from proto_fleet_sdk.telemetry.metrics import MetricValue
from proto_fleet_sdk.utils.validation import validate_device_metrics


def _make_component_info(index: int = 0) -> ComponentInfo:
    return ComponentInfo(index=index, name="test", status=ComponentStatus.COMPONENT_STATUS_HEALTHY)


def _make_valid_metrics(**overrides) -> DeviceMetrics:
    defaults = dict(
        device_id="miner-1",
        timestamp=datetime.now(timezone.utc),
        health=HEALTH_OK,
    )
    defaults.update(overrides)
    return DeviceMetrics(**defaults)


class TestValidateDeviceMetrics:
    """Tests for validate_device_metrics."""

    def test_valid_metrics_passes(self) -> None:
        validate_device_metrics(_make_valid_metrics())

    def test_empty_device_id_raises(self) -> None:
        with pytest.raises(InvalidConfigError, match="device_id"):
            validate_device_metrics(_make_valid_metrics(device_id=""))

    def test_none_timestamp_raises(self) -> None:
        with pytest.raises(InvalidConfigError, match="timestamp"):
            validate_device_metrics(_make_valid_metrics(timestamp=None))

    def test_negative_hashrate_raises(self) -> None:
        with pytest.raises(InvalidConfigError, match="hashrate_hs.*negative"):
            validate_device_metrics(_make_valid_metrics(hashrate_hs=MetricValue(value=-1.0)))

    def test_negative_power_raises(self) -> None:
        with pytest.raises(InvalidConfigError, match="power_w.*negative"):
            validate_device_metrics(_make_valid_metrics(power_w=MetricValue(value=-100.0)))

    def test_temp_out_of_range_raises(self) -> None:
        with pytest.raises(InvalidConfigError, match="temp_c.*range"):
            validate_device_metrics(_make_valid_metrics(temp_c=MetricValue(value=200.0)))

    def test_negative_board_hashrate_raises(self) -> None:
        board = HashBoardMetrics(
            component_info=_make_component_info(),
            hash_rate_hs=MetricValue(value=-5.0),
        )
        with pytest.raises(InvalidConfigError, match="hash_boards.*hash_rate_hs.*negative"):
            validate_device_metrics(_make_valid_metrics(hash_boards=[board]))

    def test_negative_psu_power_raises(self) -> None:
        psu = PSUMetrics(
            component_info=_make_component_info(),
            output_power_w=MetricValue(value=-50.0),
        )
        with pytest.raises(InvalidConfigError, match="psu_metrics.*output_power_w.*negative"):
            validate_device_metrics(_make_valid_metrics(psu_metrics=[psu]))

    def test_negative_fan_rpm_raises(self) -> None:
        fan = FanMetrics(
            component_info=_make_component_info(),
            rpm=MetricValue(value=-100.0),
        )
        with pytest.raises(InvalidConfigError, match="fan_metrics.*rpm.*negative"):
            validate_device_metrics(_make_valid_metrics(fan_metrics=[fan]))

    def test_negative_component_index_raises(self) -> None:
        board = HashBoardMetrics(component_info=_make_component_info(index=-1))
        with pytest.raises(InvalidConfigError, match="component_info.index.*negative"):
            validate_device_metrics(_make_valid_metrics(hash_boards=[board]))
