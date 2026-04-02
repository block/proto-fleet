"""Tests for the example Python plugin driver.

Demonstrates how to test a Proto Fleet plugin driver using the gRPC stubs
and proto types from the SDK.
"""

from __future__ import annotations

from unittest.mock import AsyncMock, MagicMock

import pytest
from google.protobuf.empty_pb2 import Empty
from proto_fleet_sdk.capabilities import CAP_DEVICE_STATUS, CAP_DISCOVERY, CAP_PAIRING
from proto_fleet_sdk.errors import DeviceNotFoundError
from proto_fleet_sdk.generated.pb import driver_pb2

from example_driver.driver import ExampleDriver


@pytest.fixture
def driver() -> ExampleDriver:
    return ExampleDriver()


@pytest.fixture
def mock_ctx() -> MagicMock:
    ctx = MagicMock()
    ctx.abort = AsyncMock()
    return ctx


class TestHandshake:
    async def test_returns_driver_name_and_version(self, driver: ExampleDriver, mock_ctx: MagicMock) -> None:
        # Act
        response = await driver.Handshake(Empty(), mock_ctx)

        # Assert
        assert response.driver_name == "example-python"
        assert response.api_version == "v1"


class TestDescribeDriver:
    async def test_returns_capabilities(self, driver: ExampleDriver, mock_ctx: MagicMock) -> None:
        # Act
        response = await driver.DescribeDriver(Empty(), mock_ctx)

        # Assert
        assert response.driver_name == "example-python"
        assert response.caps.flags[CAP_DISCOVERY] is True
        assert response.caps.flags[CAP_PAIRING] is True
        assert response.caps.flags[CAP_DEVICE_STATUS] is True


class TestGetDiscoveryPorts:
    async def test_returns_ports(self, driver: ExampleDriver, mock_ctx: MagicMock) -> None:
        # Act
        response = await driver.GetDiscoveryPorts(Empty(), mock_ctx)

        # Assert
        assert response.ports == ["80"]


class TestNewDevice:
    async def test_creates_device(self, driver: ExampleDriver, mock_ctx: MagicMock) -> None:
        # Arrange
        info = driver_pb2.DeviceInfo(host="192.168.1.1", port=80)
        request = driver_pb2.NewDeviceRequest(
            device_id="dev-1",
            info=info,
            secret=driver_pb2.SecretBundle(),
        )

        # Act
        response = await driver.NewDevice(request, mock_ctx)

        # Assert
        assert response.device_id == "dev-1"


class TestDeviceStatus:
    async def test_returns_metrics(self, driver: ExampleDriver, mock_ctx: MagicMock) -> None:
        # Arrange
        info = driver_pb2.DeviceInfo(host="192.168.1.1", port=80)
        await driver.NewDevice(
            driver_pb2.NewDeviceRequest(device_id="dev-1", info=info, secret=driver_pb2.SecretBundle()),
            mock_ctx,
        )

        # Act
        response = await driver.DeviceStatus(driver_pb2.DeviceRef(device_id="dev-1"), mock_ctx)

        # Assert
        assert response.device_id == "dev-1"
        assert response.health == driver_pb2.HEALTH_HEALTHY_ACTIVE

    async def test_raises_for_unknown_device(self, driver: ExampleDriver, mock_ctx: MagicMock) -> None:
        # Act & Assert
        with pytest.raises(DeviceNotFoundError):
            await driver.DeviceStatus(driver_pb2.DeviceRef(device_id="no-such"), mock_ctx)


class TestCloseDevice:
    async def test_removes_device(self, driver: ExampleDriver, mock_ctx: MagicMock) -> None:
        # Arrange
        info = driver_pb2.DeviceInfo(host="192.168.1.1", port=80)
        await driver.NewDevice(
            driver_pb2.NewDeviceRequest(device_id="dev-1", info=info, secret=driver_pb2.SecretBundle()),
            mock_ctx,
        )

        # Act
        result = await driver.CloseDevice(driver_pb2.DeviceRef(device_id="dev-1"), mock_ctx)

        # Assert
        assert isinstance(result, Empty)

    async def test_raises_for_unknown_device(self, driver: ExampleDriver, mock_ctx: MagicMock) -> None:
        # Act & Assert
        with pytest.raises(DeviceNotFoundError):
            await driver.CloseDevice(driver_pb2.DeviceRef(device_id="no-such"), mock_ctx)
