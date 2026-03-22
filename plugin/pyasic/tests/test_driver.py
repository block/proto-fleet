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

from pyasic_driver.capabilities import FW_BRAIINS, FW_STOCK, MFR_BRAIINS
from pyasic_driver.config import FirmwareConfig, MinerFamilyConfig, PluginConfig, PluginSettings
from pyasic_driver.device import PyAsicDevice
from pyasic_driver.driver import PyAsicDriver
from tests.conftest import MockMinerData, make_mock_miner


def _fw(enabled: bool = True) -> FirmwareConfig:
    return FirmwareConfig(enabled=enabled)


def _make_driver(
    config: PluginConfig | None = None,
    get_miner_fn: AsyncMock | None = None,
) -> PyAsicDriver:
    if config is None:
        config = PluginConfig(
            plugin=PluginSettings(),
            miners={"whatsminer": MinerFamilyConfig(firmware={FW_STOCK: _fw()})},
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


class TestGetDiscoveryPorts:
    async def test_returns_canonical_scan_ports(self, mock_ctx: MagicMock) -> None:
        driver = _make_driver()

        ports = await driver.get_discovery_ports(mock_ctx)

        assert ports == ["80", "443", "4028"]


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
        # Arrange
        miner = make_mock_miner(make="AntMiner", model="S19j Pro")
        config = PluginConfig(
            plugin=PluginSettings(),
            miners={"whatsminer": MinerFamilyConfig(firmware={FW_STOCK: _fw()})},
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
            miners={"whatsminer": MinerFamilyConfig(firmware={FW_STOCK: _fw()})},
        )
        driver = _make_driver(config=config, get_miner_fn=slow_get_miner)

        # Act & Assert
        with pytest.raises(DeviceUnavailableError):
            await driver.discover_device(mock_ctx, "192.168.1.100", 80)

    async def test_discover_unsupported_port_rejected(self, mock_ctx: MagicMock) -> None:
        # Arrange
        driver = _make_driver()

        # Act & Assert
        with pytest.raises(DeviceNotFoundError):
            await driver.discover_device(mock_ctx, "192.168.1.100", 2121)

    async def test_discover_on_socket_port(self, mock_ctx: MagicMock) -> None:
        # Arrange
        miner = make_mock_miner(make="WhatsMiner", model="M60S")
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))

        # Act
        info = await driver.discover_device(mock_ctx, "192.168.1.100", 4028)

        # Assert
        assert info.manufacturer == "WhatsMiner"
        assert info.port == 4028

    async def test_discover_https_on_port_443(self, mock_ctx: MagicMock) -> None:
        # Arrange
        miner = make_mock_miner(make="WhatsMiner", model="M60S")
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))

        # Act
        info = await driver.discover_device(mock_ctx, "192.168.1.100", 443)

        # Assert
        assert info.url_scheme == "https"
        assert info.port == 443

    async def test_discover_http_on_port_80(self, mock_ctx: MagicMock) -> None:
        # Arrange
        miner = make_mock_miner(make="WhatsMiner", model="M60S")
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))

        # Act
        info = await driver.discover_device(mock_ctx, "192.168.1.100", 80)

        # Assert
        assert info.url_scheme == "http"
        assert info.port == 80

    async def test_discover_multi_family(self, mock_ctx: MagicMock) -> None:
        # Arrange
        miner = make_mock_miner(make="AntMiner", model="S21")
        config = PluginConfig(
            plugin=PluginSettings(),
            miners={
                "whatsminer": MinerFamilyConfig(firmware={FW_STOCK: _fw()}),
                "antminer": MinerFamilyConfig(firmware={FW_STOCK: _fw()}),
            },
        )
        driver = _make_driver(config=config, get_miner_fn=AsyncMock(return_value=miner))

        # Act
        info = await driver.discover_device(mock_ctx, "192.168.1.100", 80)

        # Assert
        assert info.manufacturer == "AntMiner"
        assert info.model == "S21"

    async def test_discover_braiins_os_antminer(self, mock_ctx: MagicMock) -> None:
        """Braiins OS on Antminer hardware reports make=AntMiner but manufacturer=Braiins."""
        # Arrange
        miner = make_mock_miner(make="AntMiner", model="S19", braiins_os=True)
        config = PluginConfig(
            plugin=PluginSettings(),
            miners={
                "antminer": MinerFamilyConfig(firmware={
                    FW_STOCK: _fw(False),
                    FW_BRAIINS: _fw(True),
                }),
            },
        )
        driver = _make_driver(config=config, get_miner_fn=AsyncMock(return_value=miner))

        # Act
        info = await driver.discover_device(mock_ctx, "172.16.2.103", 80)

        # Assert
        assert info.manufacturer == MFR_BRAIINS
        assert info.model == "S19"

    async def test_discover_braiins_os_rejected_when_only_stock_enabled(
        self, mock_ctx: MagicMock,
    ) -> None:
        # Arrange
        miner = make_mock_miner(make="AntMiner", model="S19", braiins_os=True)
        config = PluginConfig(
            plugin=PluginSettings(),
            miners={
                "antminer": MinerFamilyConfig(firmware={
                    FW_STOCK: _fw(True),
                    FW_BRAIINS: _fw(False),
                }),
            },
        )
        driver = _make_driver(config=config, get_miner_fn=AsyncMock(return_value=miner))

        # Act & Assert
        with pytest.raises(DeviceNotFoundError):
            await driver.discover_device(mock_ctx, "172.16.2.103", 80)


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
        """get_data succeeds but write validation fails (wrong write password)."""
        # Arrange — get_data succeeds but send_config fails (wrong write password)
        from pyasic.miners.backends.btminer import BTMinerV2

        miner = make_mock_miner(spec=BTMinerV2)
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


class TestGetCapabilitiesForModel:
    async def test_returns_empty_for_unknown_model(self, mock_ctx: MagicMock) -> None:
        # Arrange
        driver = _make_driver()

        # Act
        caps = await driver.get_capabilities_for_model(mock_ctx, "UnknownModel")

        # Assert
        assert caps == {}

    async def test_returns_cached_caps_after_new_device(
        self, mock_ctx: MagicMock, mock_secret: SecretBundle,
    ) -> None:
        # Arrange
        miner = make_mock_miner(supports_power_modes=True)
        driver = _make_driver(get_miner_fn=AsyncMock(return_value=miner))
        device_info = DeviceInfo(
            host="192.168.1.100", port=80, url_scheme="http",
            serial_number="", model="M60S", manufacturer="WhatsMiner",
            mac_address="", firmware_version="",
        )
        await driver.new_device(mock_ctx, "device-1", device_info, mock_secret)

        # Act
        caps = await driver.get_capabilities_for_model(mock_ctx, "M60S")

        # Assert
        assert caps["power_mode_efficiency"] is True

    async def test_caches_false_for_bos_miner(
        self, mock_ctx: MagicMock, mock_secret: SecretBundle,
    ) -> None:
        # Arrange
        miner = make_mock_miner(
            make="AntMiner", model="S19", braiins_os=True,
            supports_power_modes=False, supports_autotuning=True,
        )
        config = PluginConfig(
            plugin=PluginSettings(),
            miners={"antminer": MinerFamilyConfig(firmware={FW_BRAIINS: _fw()})},
        )
        driver = _make_driver(config=config, get_miner_fn=AsyncMock(return_value=miner))
        device_info = DeviceInfo(
            host="192.168.1.100", port=80, url_scheme="http",
            serial_number="", model="S19", manufacturer=MFR_BRAIINS,
            mac_address="", firmware_version="",
        )
        await driver.new_device(mock_ctx, "device-1", device_info, mock_secret)

        # Act
        caps = await driver.get_capabilities_for_model(mock_ctx, "S19")

        # Assert
        assert caps["power_mode_efficiency"] is False

    async def test_firmware_variants_cached_separately(
        self, mock_ctx: MagicMock, mock_secret: SecretBundle,
    ) -> None:
        """Different firmware variants have distinct model strings and separate cache entries."""
        # Arrange — stock S19 supports power modes
        miner_stock = make_mock_miner(make="AntMiner", model="S19", supports_power_modes=True)
        driver = _make_driver(
            config=PluginConfig(
                plugin=PluginSettings(),
                miners={"antminer": MinerFamilyConfig(firmware={FW_STOCK: _fw(), FW_BRAIINS: _fw()})},
            ),
            get_miner_fn=AsyncMock(return_value=miner_stock),
        )
        info_stock = DeviceInfo(
            host="192.168.1.1", port=80, url_scheme="http",
            serial_number="", model="S19", manufacturer="AntMiner",
            mac_address="", firmware_version="",
        )
        await driver.new_device(mock_ctx, "dev-1", info_stock, mock_secret)

        # Arrange — BOS S19 (BOS+) does NOT support power modes
        miner_bos = make_mock_miner(
            make="AntMiner", model="S19 (BOS+)", braiins_os=True,
            supports_power_modes=False, supports_autotuning=True,
        )
        driver._get_miner_fn = AsyncMock(return_value=miner_bos)
        info_bos = DeviceInfo(
            host="192.168.1.2", port=80, url_scheme="http",
            serial_number="", model="S19 (BOS+)", manufacturer=MFR_BRAIINS,
            mac_address="", firmware_version="",
        )
        await driver.new_device(mock_ctx, "dev-2", info_bos, mock_secret)

        # Act
        caps_stock = await driver.get_capabilities_for_model(mock_ctx, "S19")
        caps_bos = await driver.get_capabilities_for_model(mock_ctx, "S19 (BOS+)")

        # Assert — each variant has its own capabilities
        assert caps_stock["power_mode_efficiency"] is True
        assert caps_bos["power_mode_efficiency"] is False


    async def test_offline_device_updates_caps_on_reconnect(
        self, mock_ctx: MagicMock, mock_secret: SecretBundle,
    ) -> None:
        """Device offline at new_device time populates model caps on reconnect."""
        # Arrange — probe fails during new_device (offline)
        get_miner_fn = AsyncMock(side_effect=DeviceUnavailableError("192.168.1.100"))
        driver = _make_driver(get_miner_fn=get_miner_fn)
        device_info = DeviceInfo(
            host="192.168.1.100", port=80, url_scheme="http",
            serial_number="", model="M60S", manufacturer="WhatsMiner",
            mac_address="", firmware_version="",
        )
        result = await driver.new_device(mock_ctx, "device-1", device_info, mock_secret)

        # Assert — no model caps cached yet
        assert await driver.get_capabilities_for_model(mock_ctx, "M60S") == {}

        # Arrange — device comes back online
        reconnected_miner = make_mock_miner(supports_power_modes=True)
        result.device._probe_fn = AsyncMock(return_value=reconnected_miner)
        result.device._miner = None

        # Act — trigger reconnect via status call
        await result.device.status(mock_ctx)

        # Assert — model caps now populated from reconnect
        caps = await driver.get_capabilities_for_model(mock_ctx, "M60S")
        assert caps["power_mode_efficiency"] is True


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
        # Arrange
        config = PluginConfig(
            plugin=PluginSettings(),
            miners={
                "whatsminer": MinerFamilyConfig(firmware={FW_STOCK: _fw()}),
                "auradine": MinerFamilyConfig(firmware={FW_STOCK: _fw()}),
            },
        )
        driver = _make_driver(config=config)

        # Act
        creds = await driver.get_default_credentials(mock_ctx)

        # Assert — admin/admin appears only once even though both use it
        admin_creds = [c for c in creds if c.username == "admin" and c.password == "admin"]
        assert len(admin_creds) == 1

    async def test_braiins_credentials_included(self, mock_ctx: MagicMock) -> None:
        # Arrange
        config = PluginConfig(
            plugin=PluginSettings(),
            miners={
                "antminer": MinerFamilyConfig(firmware={
                    FW_STOCK: _fw(False),
                    FW_BRAIINS: _fw(True),
                }),
            },
        )
        driver = _make_driver(config=config)

        # Act
        creds = await driver.get_default_credentials(mock_ctx)

        # Assert
        assert any(c.username == "root" and c.password == "root" for c in creds)
