"""Tests for PyAsicDriver."""

from __future__ import annotations

import asyncio
from unittest.mock import AsyncMock, MagicMock

import pytest
from proto_fleet_sdk.auth import SecretBundle
from proto_fleet_sdk.errors import (
    AuthenticationFailedError,
    DeviceNotFoundError,
    DeviceUnavailableError,
)
from proto_fleet_sdk.types import DeviceInfo

from pyasic_driver.config import MinerFamilyConfig, PluginConfig, PluginSettings
from pyasic_driver.device import PyAsicDevice
from pyasic_driver.driver import PyAsicDriver
from tests.conftest import MockMinerData, make_mock_miner


def _make_driver(
    config: PluginConfig | None = None,
    get_miner_fn: AsyncMock | None = None,
) -> PyAsicDriver:
    if config is None:
        config = PluginConfig(
            plugin=PluginSettings(),
            miners={"whatsminer": MinerFamilyConfig(enabled=True)},
        )
    if get_miner_fn is None:
        get_miner_fn = AsyncMock(return_value=make_mock_miner())
    return PyAsicDriver(config, get_miner=get_miner_fn)


class TestHandshake:
    async def test_handshake(self, mock_ctx: MagicMock) -> None:
        # Arrange
        driver = _make_driver()

        # Act
        result = await driver.handshake(mock_ctx)

        # Assert
        assert result.driver_name == "pyasic"
        assert result.api_version == "v1"


class TestDescribeDriver:
    async def test_returns_static_capabilities(self, mock_ctx: MagicMock) -> None:
        # Arrange
        driver = _make_driver()

        # Act
        identifier, caps = await driver.describe_driver(mock_ctx)

        # Assert
        assert identifier.driver_name == "pyasic"
        assert caps["discovery"] is True
        assert caps["pairing"] is True
        assert caps["device_status"] is True


class TestDiscoverDevice:
    async def test_discover_whatsminer(self, mock_ctx: MagicMock) -> None:
        # Arrange
        miner = make_mock_miner(make="WhatsMiner", model="M60S")
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))

        # Act
        info = await driver.discover_device(mock_ctx, "192.168.1.100", 80)

        # Assert
        assert info.manufacturer == "WhatsMiner"
        assert info.model == "M60S"
        assert info.host == "192.168.1.100"

    async def test_discover_disabled_manufacturer_rejected(self, mock_ctx: MagicMock) -> None:
        """Antminer is not enabled in config, so discovery should fail."""
        # Arrange
        miner = make_mock_miner(make="AntMiner", model="S19j Pro")
        config = PluginConfig(
            plugin=PluginSettings(),
            miners={"whatsminer": MinerFamilyConfig(enabled=True)},
        )
        driver = _make_driver(config=config, get_miner_fn=AsyncMock(return_value=miner))

        # Act & Assert
        with pytest.raises(DeviceNotFoundError):
            await driver.discover_device(mock_ctx, "192.168.1.200", 80)

    async def test_discover_unknown_manufacturer_rejected(self, mock_ctx: MagicMock) -> None:
        # Arrange
        miner = make_mock_miner(make="SomeUnknown", model="X1")
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))

        # Act & Assert
        with pytest.raises(DeviceNotFoundError):
            await driver.discover_device(mock_ctx, "192.168.1.200", 80)

    async def test_discover_timeout(self, mock_ctx: MagicMock) -> None:
        # Arrange
        async def slow_get_miner(ip: str) -> None:
            await asyncio.sleep(100)

        config = PluginConfig(
            plugin=PluginSettings(discovery_timeout_seconds=1),
            miners={"whatsminer": MinerFamilyConfig(enabled=True)},
        )
        driver = _make_driver(config=config, get_miner_fn=slow_get_miner)

        # Act & Assert
        with pytest.raises(DeviceUnavailableError):
            await driver.discover_device(mock_ctx, "192.168.1.100", 80)

    async def test_discover_unsupported_port_rejected(self, mock_ctx: MagicMock) -> None:
        """Port 2121 is not used by pyasic, so discovery should fail immediately."""
        # Arrange
        driver = _make_driver()

        # Act & Assert
        with pytest.raises(DeviceNotFoundError):
            await driver.discover_device(mock_ctx, "192.168.1.100", 2121)

    async def test_discover_on_socket_port(self, mock_ctx: MagicMock) -> None:
        """Port 4028 is a valid pyasic socket detection port."""
        # Arrange
        miner = make_mock_miner(make="WhatsMiner", model="M60S")
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))

        # Act
        info = await driver.discover_device(mock_ctx, "192.168.1.100", 4028)

        # Assert
        assert info.manufacturer == "WhatsMiner"
        assert info.port == 4028

    async def test_discover_https_on_port_443(self, mock_ctx: MagicMock) -> None:
        """Port 443 should set url_scheme to https."""
        # Arrange
        miner = make_mock_miner(make="WhatsMiner", model="M60S")
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))

        # Act
        info = await driver.discover_device(mock_ctx, "192.168.1.100", 443)

        # Assert
        assert info.url_scheme == "https"
        assert info.port == 443

    async def test_discover_http_on_port_80(self, mock_ctx: MagicMock) -> None:
        """Port 80 should set url_scheme to http."""
        # Arrange
        miner = make_mock_miner(make="WhatsMiner", model="M60S")
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))

        # Act
        info = await driver.discover_device(mock_ctx, "192.168.1.100", 80)

        # Assert
        assert info.url_scheme == "http"
        assert info.port == 80

    async def test_discover_multi_family(self, mock_ctx: MagicMock) -> None:
        """Both WhatsMiner and Antminer enabled — Antminer should be accepted."""
        # Arrange
        miner = make_mock_miner(make="AntMiner", model="S21")
        config = PluginConfig(
            plugin=PluginSettings(),
            miners={
                "whatsminer": MinerFamilyConfig(enabled=True),
                "antminer": MinerFamilyConfig(enabled=True),
            },
        )
        driver = _make_driver(config=config, get_miner_fn=AsyncMock(return_value=miner))

        # Act
        info = await driver.discover_device(mock_ctx, "192.168.1.100", 80)

        # Assert
        assert info.manufacturer == "AntMiner"
        assert info.model == "S21"


class TestPairDevice:
    async def test_pair_extracts_mac(self, mock_ctx: MagicMock, mock_secret: SecretBundle) -> None:
        # Arrange
        data = MockMinerData(mac="AA:BB:CC:DD:EE:FF", fw_ver="2.0.0")
        miner = make_mock_miner(data=data)
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))
        device_info = DeviceInfo(
            host="192.168.1.100", port=80, url_scheme="http",
            serial_number="", model="M60S", manufacturer="WhatsMiner",
            mac_address="", firmware_version="1.0.0",
        )

        # Act
        result = await driver.pair_device(mock_ctx, device_info, mock_secret)

        # Assert
        assert result.mac_address == "AA:BB:CC:DD:EE:FF"
        assert result.firmware_version == "2.0.0"

    async def test_pair_auth_failure(self, mock_ctx: MagicMock, mock_secret: SecretBundle) -> None:
        # Arrange
        miner = make_mock_miner()
        miner.get_data = AsyncMock(side_effect=Exception("auth failed"))
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))
        device_info = DeviceInfo(
            host="192.168.1.100", port=80, url_scheme="http",
            serial_number="", model="M60S", manufacturer="WhatsMiner",
            mac_address="", firmware_version="",
        )

        # Act & Assert
        with pytest.raises(AuthenticationFailedError):
            await driver.pair_device(mock_ctx, device_info, mock_secret)

    async def test_pair_rejects_wrong_write_credentials(
        self, mock_ctx: MagicMock, mock_secret: SecretBundle,
    ) -> None:
        # Arrange — get_data succeeds but send_config fails (wrong write password)
        miner = make_mock_miner()
        miner.send_config = AsyncMock(side_effect=Exception("invalid password"))
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))
        device_info = DeviceInfo(
            host="192.168.1.100", port=80, url_scheme="http",
            serial_number="", model="M60S", manufacturer="WhatsMiner",
            mac_address="", firmware_version="",
        )

        # Act & Assert
        with pytest.raises(AuthenticationFailedError):
            await driver.pair_device(mock_ctx, device_info, mock_secret)

    async def test_pair_applies_rpc_credentials(
        self, mock_ctx: MagicMock, mock_secret: SecretBundle,
    ) -> None:
        # Arrange
        miner = make_mock_miner()
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))
        device_info = DeviceInfo(
            host="192.168.1.100", port=80, url_scheme="http",
            serial_number="", model="M60S", manufacturer="WhatsMiner",
            mac_address="", firmware_version="",
        )

        # Act
        await driver.pair_device(mock_ctx, device_info, mock_secret)

        # Assert — RPC password was set from the credential
        assert miner.rpc.pwd == mock_secret.kind.password


class TestNewDevice:
    async def test_creates_device(self, mock_ctx: MagicMock, mock_secret: SecretBundle) -> None:
        # Arrange
        miner = make_mock_miner()
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))
        device_info = DeviceInfo(
            host="192.168.1.100", port=80, url_scheme="http",
            serial_number="", model="M60S", manufacturer="WhatsMiner",
            mac_address="", firmware_version="",
        )

        # Act
        result = await driver.new_device(mock_ctx, "device-1", device_info, mock_secret)

        # Assert
        assert isinstance(result.device, PyAsicDevice)
        assert result.device.id() == "device-1"


class TestDefaultCredentials:
    async def test_single_family(self, mock_ctx: MagicMock) -> None:
        # Arrange
        driver = _make_driver()

        # Act
        creds = await driver.get_default_credentials(mock_ctx)

        # Assert
        assert len(creds) >= 1
        assert any(c.username == "admin" and c.password == "admin" for c in creds)

    async def test_multi_family_deduplicates(self, mock_ctx: MagicMock) -> None:
        """If multiple families share the same creds, they should be deduped."""
        # Arrange
        config = PluginConfig(
            plugin=PluginSettings(),
            miners={
                "whatsminer": MinerFamilyConfig(enabled=True),
                "auradine": MinerFamilyConfig(enabled=True),
            },
        )
        driver = _make_driver(config=config)

        # Act
        creds = await driver.get_default_credentials(mock_ctx)

        # Assert — admin/admin appears only once even though both use it
        admin_creds = [c for c in creds if c.username == "admin" and c.password == "admin"]
        assert len(admin_creds) == 1
