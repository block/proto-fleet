"""Tests for the generic PyAsicDevice."""

from __future__ import annotations

from unittest.mock import AsyncMock, MagicMock

import pytest
from proto_fleet_sdk.error_codes import ComponentType, MinerError, Severity
from proto_fleet_sdk.errors import DeviceUnavailableError, UnsupportedCapabilityError
from proto_fleet_sdk.generated.pb import driver_pb2

from pyasic_driver.device import DeviceCommandFailedError, PyAsicDevice, _infer_component, _infer_severity
from tests.conftest import (
    MockFan,
    MockHashBoard,
    MockMinerData,
    MockMinerError,
    make_mock_miner,
)

# Unit conversion constant (inlined from removed converters)
_THS_TO_HS = 1e12

DEVICE_INFO = driver_pb2.DeviceInfo(
    host="192.168.1.100",
    port=80,
    url_scheme="http",
    serial_number="",
    model="M60S",
    manufacturer="WhatsMiner",
    mac_address="",
    firmware_version="1.0.0",
)

ALL_CAPS = {
    "mining_start": True, "mining_stop": True, "reboot": True, "led_blink": True,
    "get_mining_pools": True, "update_mining_pools": True, "firmware": True,
    "device_status": True, "get_errors": True, "power_mode_efficiency": True,
}

NO_CONTROL_CAPS = {
    "mining_start": False, "mining_stop": False, "reboot": False, "led_blink": False,
    "device_status": True, "get_errors": True,
}


def _make_device(miner: MagicMock, caps: dict | None = None) -> PyAsicDevice:
    return PyAsicDevice(
        device_id="test-device-1",
        miner=miner,
        device_info=DEVICE_INFO,
        caps=caps or ALL_CAPS,
        cache_ttl_seconds=5,
    )


class TestDeviceCore:
    def test_id(self) -> None:
        # Arrange
        device = _make_device(make_mock_miner())

        # Act & Assert
        assert device.id() == "test-device-1"

    def test_describe_device(self) -> None:
        # Arrange
        device = _make_device(make_mock_miner())

        # Act
        info, caps = device.describe_device()

        # Assert
        assert info == DEVICE_INFO
        assert caps == ALL_CAPS

    async def test_close_clears_cache_and_miner(self) -> None:
        # Arrange
        device = _make_device(make_mock_miner())
        await device.status()  # populate cache

        # Act
        device.close()

        # Assert
        assert device._last_status is None
        assert device._miner is None


class TestTelemetry:
    async def test_status_returns_metrics(self) -> None:
        # Arrange
        data = MockMinerData(hashrate=110.5, wattage=3200.0, temperature_avg=65.0, efficiency=29.0)
        miner = make_mock_miner(data=data)
        device = _make_device(miner)

        # Act
        metrics = await device.status()

        # Assert
        assert metrics.health == driver_pb2.HEALTH_HEALTHY_ACTIVE
        assert metrics.hashrate_hs.value == pytest.approx(110.5 * _THS_TO_HS)
        assert metrics.hashrate_hs.kind == driver_pb2.METRIC_KIND_RATE
        assert metrics.power_w.value == pytest.approx(3200.0)
        assert metrics.temp_c.value == pytest.approx(65.0)

    async def test_status_caching(self) -> None:
        # Arrange
        miner = make_mock_miner()
        device = _make_device(miner)

        # Act
        await device.status()
        await device.status()

        # Assert — get_data called only once due to caching
        assert miner.get_data.call_count == 1

    async def test_status_communication_failure(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.get_data = AsyncMock(side_effect=Exception("connection refused"))
        device = _make_device(miner)

        # Act
        metrics = await device.status()

        # Assert
        assert metrics.health == driver_pb2.HEALTH_CRITICAL
        assert metrics.health_reason == "Failed to communicate with device"
        assert device._miner is None  # cleared for reconnect on next call

    async def test_status_communication_failure_triggers_reconnect(self) -> None:
        # Arrange — device starts connected, then get_data fails
        miner = make_mock_miner()
        miner.get_data = AsyncMock(side_effect=Exception("connection refused"))
        reconnected_miner = make_mock_miner()
        probe_fn = AsyncMock(return_value=reconnected_miner)
        device = PyAsicDevice(
            device_id="test-device-1",
            miner=miner,
            device_info=DEVICE_INFO,
            caps=ALL_CAPS,
            cache_ttl_seconds=0,
            probe_fn=probe_fn,
        )

        # Act — first call fails and clears _miner
        metrics = await device.status()
        assert metrics.health == driver_pb2.HEALTH_CRITICAL
        assert device._miner is None

        # Act — second call triggers reconnect via probe_fn
        metrics = await device.status()

        # Assert
        probe_fn.assert_called_once_with(DEVICE_INFO.host)
        assert device._miner is reconnected_miner
        assert metrics.health == driver_pb2.HEALTH_HEALTHY_ACTIVE

    async def test_reconnect_rejects_mismatched_device(self) -> None:
        # Arrange — probe returns a different device at the same IP
        wrong_miner = make_mock_miner(make="Antminer", model="S19")
        probe_fn = AsyncMock(return_value=wrong_miner)
        device = PyAsicDevice(
            device_id="test-device-1",
            miner=None,
            device_info=DEVICE_INFO,  # expects WhatsMiner/M60S
            caps=ALL_CAPS,
            cache_ttl_seconds=0,
            probe_fn=probe_fn,
        )

        # Act & Assert — should raise because reconnect fails identity check
        with pytest.raises(DeviceUnavailableError):
            await device.status()
        assert device._miner is None

    async def test_hashboard_conversion(self) -> None:
        # Arrange
        data = MockMinerData(
            hashboards=[
                MockHashBoard(hashrate=37.0, temp=65.0, chips=114, expected_chips=114),
                MockHashBoard(hashrate=0, temp=0, chips=0),
            ]
        )
        miner = make_mock_miner(data=data)
        device = _make_device(miner)

        # Act
        metrics = await device.status()

        # Assert
        assert len(metrics.hash_boards) == 2
        board0 = metrics.hash_boards[0]
        assert board0.component_info.status == driver_pb2.COMPONENT_STATUS_HEALTHY
        assert board0.hash_rate_hs.value == pytest.approx(37.0 * _THS_TO_HS)
        assert board0.chip_count == 114

        board1 = metrics.hash_boards[1]
        assert board1.component_info.status == driver_pb2.COMPONENT_STATUS_OFFLINE

    async def test_fan_conversion(self) -> None:
        # Arrange
        data = MockMinerData(fans=[MockFan(speed=4200), MockFan(speed=0)])
        miner = make_mock_miner(data=data)
        device = _make_device(miner)

        # Act
        metrics = await device.status()

        # Assert — fan with speed=0 is filtered out
        assert len(metrics.fan_metrics) == 1
        assert metrics.fan_metrics[0].rpm.value == 4200.0

    async def test_psu_conversion(self) -> None:
        # Arrange
        data = MockMinerData(wattage=3200.0, voltage=12.5, current=256.0)
        miner = make_mock_miner(data=data)
        device = _make_device(miner)

        # Act
        metrics = await device.status()

        # Assert
        assert len(metrics.psu_metrics) == 1
        psu = metrics.psu_metrics[0]
        assert psu.output_power_w.value == pytest.approx(3200.0)
        assert psu.output_voltage_v.value == pytest.approx(12.5)


class TestHealth:
    async def test_mining_active_healthy(self) -> None:
        # Arrange
        data = MockMinerData(is_mining=True, hashrate=110.0)
        device = _make_device(make_mock_miner(data=data))

        # Act
        metrics = await device.status()

        # Assert
        assert metrics.health == driver_pb2.HEALTH_HEALTHY_ACTIVE

    async def test_mining_no_hashrate_warning(self) -> None:
        # Arrange
        data = MockMinerData(is_mining=True, hashrate=0)
        device = _make_device(make_mock_miner(data=data))

        # Act
        metrics = await device.status()

        # Assert
        assert metrics.health == driver_pb2.HEALTH_WARNING

    async def test_not_mining_inactive(self) -> None:
        # Arrange
        data = MockMinerData(is_mining=False, hashrate=0)
        device = _make_device(make_mock_miner(data=data))

        # Act
        metrics = await device.status()

        # Assert
        assert metrics.health == driver_pb2.HEALTH_HEALTHY_INACTIVE

    async def test_errors_cause_warning(self) -> None:
        # Arrange
        data = MockMinerData(
            is_mining=True,
            hashrate=110.0,
            errors=[MockMinerError(error_code=1, error_message="Fan speed deviation")],
        )
        device = _make_device(make_mock_miner(data=data))

        # Act
        metrics = await device.status()

        # Assert
        assert metrics.health == driver_pb2.HEALTH_WARNING

    async def test_stopped_with_errors_is_inactive(self) -> None:
        # Arrange — miner is stopped but has stale error codes
        data = MockMinerData(
            is_mining=False,
            hashrate=0,
            errors=[MockMinerError(error_code=9022, error_message="Unknown error type.")],
        )
        device = _make_device(make_mock_miner(data=data))

        # Act
        metrics = await device.status()

        # Assert — inactive takes priority over stale errors
        assert metrics.health == driver_pb2.HEALTH_HEALTHY_INACTIVE


class TestControl:
    async def test_start_mining(self) -> None:
        # Arrange
        miner = make_mock_miner()
        device = _make_device(miner)

        # Act
        await device.start_mining()

        # Assert
        miner.resume_mining.assert_called_once()

    async def test_stop_mining(self) -> None:
        # Arrange
        miner = make_mock_miner()
        device = _make_device(miner)

        # Act
        await device.stop_mining()

        # Assert
        miner.stop_mining.assert_called_once()

    async def test_reboot(self) -> None:
        # Arrange
        miner = make_mock_miner()
        device = _make_device(miner)

        # Act
        await device.reboot()

        # Assert
        miner.reboot.assert_called_once()

    async def test_blink_led_turns_on_and_schedules_off(self) -> None:
        # Arrange
        miner = make_mock_miner()
        device = _make_device(miner)

        # Act
        await device.blink_led()

        # Assert
        miner.fault_light_on.assert_called_once()
        # fault_light_off is scheduled via call_later, not called immediately
        miner.fault_light_off.assert_not_called()


class TestControlFailure:
    """Verify that commands raise DeviceCommandFailedError when pyasic returns False."""

    async def test_start_mining_failure_raises(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.resume_mining = AsyncMock(return_value=False)
        device = _make_device(miner)

        # Act & Assert
        with pytest.raises(DeviceCommandFailedError, match="resume_mining"):
            await device.start_mining()

    async def test_stop_mining_failure_raises(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.stop_mining = AsyncMock(return_value=False)
        device = _make_device(miner)

        # Act & Assert
        with pytest.raises(DeviceCommandFailedError, match="stop_mining"):
            await device.stop_mining()

    async def test_reboot_failure_raises(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.reboot = AsyncMock(return_value=False)
        device = _make_device(miner)

        # Act & Assert
        with pytest.raises(DeviceCommandFailedError, match="reboot"):
            await device.reboot()

    async def test_blink_led_failure_raises(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.fault_light_on = AsyncMock(return_value=False)
        device = _make_device(miner)

        # Act & Assert
        with pytest.raises(DeviceCommandFailedError, match="fault_light_on"):
            await device.blink_led()

    async def test_firmware_update_failure_raises(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.upgrade_firmware = AsyncMock(return_value=False)
        device = _make_device(miner)

        # Act & Assert
        with pytest.raises(DeviceCommandFailedError, match="upgrade_firmware"):
            await device.firmware_update()


class TestBenignAPIErrorHandling:
    """Verify that _exec_pyasic_command treats known-benign APIErrors as success."""

    @pytest.mark.parametrize("error_msg", [
        "JSON decode error for 'mining/stop' from 172.16.2.103: Expecting value",
        "HTTP error sending 'mining/stop' to 172.16.2.103: ReadTimeout",
        "Attribute error sending 'reboot' to 172.16.2.103: NoneType",
        "Failed to send command to miner: 172.16.2.103",
        "No data was returned from the API.",
    ])
    async def test_benign_api_error_treated_as_success(self, error_msg: str) -> None:
        # Arrange
        from pyasic.errors import APIError

        miner = make_mock_miner()
        miner.stop_mining = AsyncMock(side_effect=APIError(error_msg))
        device = _make_device(miner)

        # Act — should not raise
        await device.stop_mining()

    async def test_unknown_api_error_raises_device_unavailable(self) -> None:
        # Arrange
        from pyasic.errors import APIError

        miner = make_mock_miner()
        miner.reboot = AsyncMock(
            side_effect=APIError("Could not authenticate web token with miner: 172.16.2.103"),
        )
        device = _make_device(miner)

        # Act & Assert
        with pytest.raises(DeviceUnavailableError):
            await device.reboot()

    async def test_non_disruptive_benign_api_error_still_raises_device_unavailable(self) -> None:
        # Arrange
        from pyasic.errors import APIError

        miner = make_mock_miner()
        miner.upgrade_firmware = AsyncMock(
            side_effect=APIError("Failed to send command to miner: 172.16.2.103"),
        )
        device = _make_device(miner)

        # Act & Assert
        with pytest.raises(DeviceUnavailableError):
            await device.firmware_update()

    @pytest.mark.parametrize("method_name, device_call", [
        ("resume_mining", lambda device: device.start_mining()),
        ("stop_mining", lambda device: device.stop_mining()),
        ("reboot", lambda device: device.reboot()),
    ])
    @pytest.mark.parametrize("error_factory", [
        lambda: TimeoutError("timeout"),
        lambda: type("ReadTimeout", (Exception,), {})("read timed out"),
    ])
    async def test_disruptive_timeout_exception_treated_as_success(
        self,
        method_name: str,
        device_call: object,
        error_factory: object,
    ) -> None:
        # Arrange
        miner = make_mock_miner()
        getattr(miner, method_name).side_effect = error_factory()
        device = _make_device(miner)

        # Act - should not raise
        await device_call(device)

    async def test_non_disruptive_timeout_exception_still_raises(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.fault_light_on = AsyncMock(side_effect=TimeoutError("timeout"))
        device = _make_device(miner)

        # Act & Assert
        with pytest.raises(TimeoutError):
            await device.blink_led()


class TestSetPowerTarget:
    async def test_maximum_hashrate_sends_hpm(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.get_config = AsyncMock(return_value=MagicMock(as_dict=MagicMock(return_value={})))
        device = _make_device(miner)

        # Act
        await device.set_power_target(driver_pb2.PERFORMANCE_MODE_MAXIMUM_HASHRATE)

        # Assert
        miner.send_config.assert_called_once()
        sent_config = miner.send_config.call_args[0][0]
        assert sent_config.mining_mode.mode == "high"

    async def test_efficiency_sends_lpm(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.get_config = AsyncMock(return_value=MagicMock(as_dict=MagicMock(return_value={})))
        device = _make_device(miner)

        # Act
        await device.set_power_target(driver_pb2.PERFORMANCE_MODE_EFFICIENCY)

        # Assert
        miner.send_config.assert_called_once()
        sent_config = miner.send_config.call_args[0][0]
        assert sent_config.mining_mode.mode == "low"

    async def test_unspecified_sends_normal(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.get_config = AsyncMock(return_value=MagicMock(as_dict=MagicMock(return_value={})))
        device = _make_device(miner)

        # Act
        await device.set_power_target(driver_pb2.PERFORMANCE_MODE_UNSPECIFIED)

        # Assert
        miner.send_config.assert_called_once()
        sent_config = miner.send_config.call_args[0][0]
        assert sent_config.mining_mode.mode == "normal"

    async def test_blocked_without_capability(self) -> None:
        # Arrange
        device = _make_device(make_mock_miner(), caps=NO_CONTROL_CAPS)

        # Act & Assert
        with pytest.raises(UnsupportedCapabilityError):
            await device.set_power_target(driver_pb2.PERFORMANCE_MODE_MAXIMUM_HASHRATE)


class TestSetPowerTargetPreset:
    """Verify set_power_target for preset-based miners (VNish)."""

    def _make_api_preset(self, power: int, tuned: bool = True) -> dict:
        return {
            "name": f"preset_{power}",
            "pretty": f"{power} watt ~ 100 TH",
            "status": "tuned" if tuned else "not_tuned",
            "modded_psu_required": False,
        }

    def _make_preset_miner(self, api_presets: list[dict]) -> MagicMock:
        miner = make_mock_miner(supports_presets=True)
        miner.web.autotune_presets = AsyncMock(return_value=api_presets)
        miner.set_power_limit = AsyncMock(return_value=True)
        return miner

    async def test_max_hashrate_selects_highest_preset(self) -> None:
        # Arrange
        presets = [self._make_api_preset(1000), self._make_api_preset(1500), self._make_api_preset(2000)]
        miner = self._make_preset_miner(presets)
        device = _make_device(miner)

        # Act
        await device.set_power_target(driver_pb2.PERFORMANCE_MODE_MAXIMUM_HASHRATE)

        # Assert
        miner.set_power_limit.assert_called_once_with(2000)

    async def test_efficiency_selects_lowest_preset(self) -> None:
        # Arrange
        presets = [self._make_api_preset(1000), self._make_api_preset(1500), self._make_api_preset(2000)]
        miner = self._make_preset_miner(presets)
        device = _make_device(miner)

        # Act
        await device.set_power_target(driver_pb2.PERFORMANCE_MODE_EFFICIENCY)

        # Assert
        miner.set_power_limit.assert_called_once_with(1000)

    async def test_unspecified_selects_median_preset(self) -> None:
        # Arrange
        presets = [self._make_api_preset(1000), self._make_api_preset(1500), self._make_api_preset(2000)]
        miner = self._make_preset_miner(presets)
        device = _make_device(miner)

        # Act
        await device.set_power_target(driver_pb2.PERFORMANCE_MODE_UNSPECIFIED)

        # Assert
        miner.set_power_limit.assert_called_once_with(1500)

    async def test_ignores_untuned_presets(self) -> None:
        # Arrange
        presets = [
            self._make_api_preset(500, tuned=False),
            self._make_api_preset(1000),
            self._make_api_preset(2000),
        ]
        miner = self._make_preset_miner(presets)
        device = _make_device(miner)

        # Act
        await device.set_power_target(driver_pb2.PERFORMANCE_MODE_EFFICIENCY)

        # Assert
        miner.set_power_limit.assert_called_once_with(1000)

    async def test_fails_when_no_tuned_presets(self) -> None:
        # Arrange
        presets = [self._make_api_preset(1000, tuned=False)]
        miner = self._make_preset_miner(presets)
        device = _make_device(miner)

        # Act & Assert
        with pytest.raises(UnsupportedCapabilityError):
            await device.set_power_target(driver_pb2.PERFORMANCE_MODE_MAXIMUM_HASHRATE)

    async def test_works_when_miner_in_manual_mode(self) -> None:
        """Presets are fetched from API directly, so manual mode doesn't matter."""
        # Arrange
        presets = [self._make_api_preset(1000), self._make_api_preset(2000)]
        miner = self._make_preset_miner(presets)
        device = _make_device(miner)

        # Act
        await device.set_power_target(driver_pb2.PERFORMANCE_MODE_MAXIMUM_HASHRATE)

        # Assert
        miner.set_power_limit.assert_called_once_with(2000)

    async def test_handles_presets_wrapped_in_dict(self) -> None:
        """Some firmware versions wrap presets in {"presets": [...]}."""
        # Arrange
        raw_presets = [self._make_api_preset(1000), self._make_api_preset(1500)]
        miner = make_mock_miner(supports_presets=True)
        miner.web.autotune_presets = AsyncMock(return_value={"presets": raw_presets})
        miner.set_power_limit = AsyncMock(return_value=True)
        device = _make_device(miner)

        # Act
        await device.set_power_target(driver_pb2.PERFORMANCE_MODE_EFFICIENCY)

        # Assert
        miner.set_power_limit.assert_called_once_with(1000)


class TestCapabilityGating:
    async def test_start_mining_blocked(self) -> None:
        # Arrange
        device = _make_device(make_mock_miner(), caps=NO_CONTROL_CAPS)

        # Act & Assert
        with pytest.raises(UnsupportedCapabilityError):
            await device.start_mining()

    async def test_reboot_blocked(self) -> None:
        # Arrange
        device = _make_device(make_mock_miner(), caps=NO_CONTROL_CAPS)

        # Act & Assert
        with pytest.raises(UnsupportedCapabilityError):
            await device.reboot()

    async def test_set_cooling_mode_always_unsupported(self) -> None:
        # Arrange — cooling mode is raised at the driver level, not device
        from pyasic_driver.config import FirmwareConfig, MinerFamilyConfig, PluginConfig, PluginSettings
        from pyasic_driver.driver import PyAsicDriver
        driver = PyAsicDriver(
            config=PluginConfig(
                plugin=PluginSettings(),
                miners={"whatsminer": MinerFamilyConfig(firmware={"stock": FirmwareConfig(enabled=True)})},
            ),
            get_miner=AsyncMock(return_value=make_mock_miner()),
        )
        mock_ctx = MagicMock()  # needs abort() method for grpc_error_handler
        mock_ctx.abort = AsyncMock()
        mock_secret = driver_pb2.SecretBundle(
            version="1",
            user_pass=driver_pb2.UsernamePassword(username="admin", password="admin"),
        )
        await driver.NewDevice(
            driver_pb2.NewDeviceRequest(device_id="dev-1", info=DEVICE_INFO, secret=mock_secret),
            mock_ctx,
        )

        # Act & Assert
        with pytest.raises(UnsupportedCapabilityError):
            await driver.SetCoolingMode(
                driver_pb2.SetCoolingModeRequest(
                    ref=driver_pb2.DeviceRef(device_id="dev-1"),
                    mode=1,
                ),
                mock_ctx,
            )

    async def test_update_miner_password_always_unsupported(self) -> None:
        # Arrange — password update is raised at the driver level
        from pyasic_driver.config import FirmwareConfig, MinerFamilyConfig, PluginConfig, PluginSettings
        from pyasic_driver.driver import PyAsicDriver
        driver = PyAsicDriver(
            config=PluginConfig(
                plugin=PluginSettings(),
                miners={"whatsminer": MinerFamilyConfig(firmware={"stock": FirmwareConfig(enabled=True)})},
            ),
            get_miner=AsyncMock(return_value=make_mock_miner()),
        )
        mock_ctx = MagicMock()  # needs abort() method for grpc_error_handler
        mock_ctx.abort = AsyncMock()
        mock_secret = driver_pb2.SecretBundle(
            version="1",
            user_pass=driver_pb2.UsernamePassword(username="admin", password="admin"),
        )
        await driver.NewDevice(
            driver_pb2.NewDeviceRequest(device_id="dev-1", info=DEVICE_INFO, secret=mock_secret),
            mock_ctx,
        )

        # Act & Assert
        with pytest.raises(UnsupportedCapabilityError):
            await driver.UpdateMinerPassword(
                driver_pb2.UpdateMinerPasswordRequest(
                    ref=driver_pb2.DeviceRef(device_id="dev-1"),
                    current_password="old",
                    new_password="new",
                ),
                mock_ctx,
            )


class TestErrorReporting:
    async def test_no_errors(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.get_errors = AsyncMock(return_value=[])
        device = _make_device(miner)

        # Act
        result = await device.get_errors()

        # Assert
        assert len(result.errors) == 0

    async def test_errors_mapped_by_code_range(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.get_errors = AsyncMock(return_value=[
            MockMinerError(error_code=100, error_message="Fan unknown."),
        ])
        device = _make_device(miner)

        # Act
        result = await device.get_errors()

        # Assert
        assert len(result.errors) == 1
        err = result.errors[0]
        assert err.miner_error == MinerError.FAN_FAILED
        assert err.summary == "Fan unknown."
        assert err.vendor_attributes["vendor_error_code"] == "100"

    async def test_errors_mapped_by_keyword_fallback(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.get_errors = AsyncMock(return_value=[
            MockMinerError(error_code=None, error_message="Environment temperature is too high"),
        ])
        device = _make_device(miner)

        # Act
        result = await device.get_errors()

        # Assert
        assert len(result.errors) == 1
        err = result.errors[0]
        assert err.miner_error == MinerError.DEVICE_OVER_TEMPERATURE

    async def test_errors_unrecognized_falls_back_to_unmapped(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.get_errors = AsyncMock(return_value=[
            MockMinerError(error_code=99999, error_message="Something completely unknown"),
        ])
        device = _make_device(miner)

        # Act
        result = await device.get_errors()

        # Assert
        assert len(result.errors) == 1
        assert result.errors[0].miner_error == MinerError.VENDOR_ERROR_UNMAPPED

    async def test_error_get_failure_returns_empty_and_clears_miner(self) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.get_errors = AsyncMock(side_effect=Exception("timeout"))
        device = _make_device(miner)

        # Act
        result = await device.get_errors()

        # Assert
        assert len(result.errors) == 0
        assert device._miner is None


class TestInferSeverity:
    def test_critical_keywords(self) -> None:
        assert _infer_severity("Over temperature protection triggered") == Severity.SEVERITY_CRITICAL
        assert _infer_severity("PSU fault detected") == Severity.SEVERITY_CRITICAL
        assert _infer_severity("Power overcurrent") == Severity.SEVERITY_CRITICAL

    def test_minor_keywords(self) -> None:
        assert _infer_severity("Fan speed deviation") == Severity.SEVERITY_MINOR
        assert _infer_severity("Ambient temperature warning") == Severity.SEVERITY_MINOR

    def test_default_major(self) -> None:
        assert _infer_severity("Some unknown error") == Severity.SEVERITY_MAJOR


class TestInferComponent:
    def test_fan(self) -> None:
        assert _infer_component("Fan speed too low") == ComponentType.COMPONENT_TYPE_FAN

    def test_hashboard(self) -> None:
        assert _infer_component("Hashboard chip count low") == ComponentType.COMPONENT_TYPE_HASH_BOARD
        assert _infer_component("ASIC chain error") == ComponentType.COMPONENT_TYPE_HASH_BOARD

    def test_psu(self) -> None:
        assert _infer_component("Power supply fault") == ComponentType.COMPONENT_TYPE_PSU
        assert _infer_component("Voltage too high") == ComponentType.COMPONENT_TYPE_PSU

    def test_eeprom(self) -> None:
        assert _infer_component("EEPROM checksum mismatch") == ComponentType.COMPONENT_TYPE_EEPROM

    def test_unknown(self) -> None:
        assert _infer_component("Something happened") == ComponentType.COMPONENT_TYPE_UNSPECIFIED
