"""Shared test fixtures for the pyasic plugin."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any
from unittest.mock import AsyncMock, MagicMock

import pytest
from proto_fleet_sdk.generated.pb import driver_pb2

from pyasic_driver.capabilities import FW_STOCK
from pyasic_driver.config import FirmwareConfig, MinerFamilyConfig, PluginConfig, PluginSettings
from pyasic_driver.patches import btminer

btminer.apply()

# --- Mock pyasic data structures ---


@dataclass
class MockHashBoard:
    hashrate: float | None = None
    temp: float | None = None
    chips: int | None = None
    expected_chips: int | None = None
    chip_freq: float | None = None
    serial_number: str = ""


@dataclass
class MockFan:
    speed: int | None = None


@dataclass
class MockPool:
    url: str = ""
    user: str = ""
    password: str = ""


@dataclass
class MockPoolGroup:
    pools: list[MockPool] = field(default_factory=list)
    quota: int = 1


@dataclass
class MockPoolConfig:
    groups: list[MockPoolGroup] = field(default_factory=list)


@dataclass
class MockMinerConfig:
    pools: MockPoolConfig | None = None

    def as_dict(self) -> dict[str, Any]:
        return {}


@dataclass
class MockMinerError:
    error_code: int | None = None
    error_message: str = ""


@dataclass
class MockMinerData:
    ip: str = "192.168.1.100"
    is_mining: bool | None = True
    hashrate: float | None = 110.5
    wattage: float | None = 3200.0
    temperature_avg: float | None = 65.0
    efficiency: float | None = 29.0
    voltage: float | None = 12.5
    current: float | None = 256.0
    mac: str = "AA:BB:CC:DD:EE:FF"
    fw_ver: str = "1.0.0"
    hashboards: list[MockHashBoard] = field(default_factory=list)
    fans: list[MockFan] = field(default_factory=list)
    errors: list[MockMinerError] = field(default_factory=list)


class BraiinsOSFirmware:
    """Stub for pyasic's BraiinsOSFirmware MRO class used in firmware detection."""


class _BOSMinerMock(BraiinsOSFirmware, MagicMock):
    """Mock whose MRO includes BraiinsOSFirmware for detect_firmware_variant."""


def make_mock_miner(
    *,
    make: str = "WhatsMiner",
    model: str = "M60S",
    fw_ver: str = "1.0.0",
    supports_shutdown: bool = True,
    supports_power_modes: bool = False,
    supports_presets: bool = False,
    supports_autotuning: bool = False,
    data: MockMinerData | None = None,
    braiins_os: bool = False,
    spec: type | None = None,
) -> MagicMock:
    """Create a mock pyasic miner for testing."""
    if braiins_os:
        miner = _BOSMinerMock()
    elif spec is not None:
        miner = MagicMock(spec=spec)
    else:
        miner = MagicMock()
    miner.make = make
    miner.model = model
    miner.fw_ver = fw_ver
    miner.raw_model = model
    miner.firmware = None
    miner.supports_shutdown = supports_shutdown
    miner.supports_power_modes = supports_power_modes
    miner.supports_presets = supports_presets
    miner.supports_autotuning = supports_autotuning

    miner.rpc = MagicMock()
    miner.web = MagicMock()

    mock_data = data or MockMinerData()
    miner.get_data = AsyncMock(return_value=mock_data)
    miner.get_errors = AsyncMock(return_value=mock_data.errors)
    miner.get_config = AsyncMock(return_value=MockMinerConfig())

    miner.reboot = AsyncMock(return_value=True)
    miner.fault_light_on = AsyncMock(return_value=True)
    miner.fault_light_off = AsyncMock(return_value=True)
    miner.stop_mining = AsyncMock(return_value=True)
    miner.resume_mining = AsyncMock(return_value=True)
    miner.send_config = AsyncMock(return_value=None)
    miner.upgrade_firmware = AsyncMock(return_value=True)

    return miner


@pytest.fixture
def mock_miner() -> MagicMock:
    return make_mock_miner()


@pytest.fixture
def mock_miner_data() -> MockMinerData:
    return MockMinerData(
        hashboards=[
            MockHashBoard(hashrate=37.0, temp=65.0, chips=114, expected_chips=114),
            MockHashBoard(hashrate=36.5, temp=67.0, chips=114, expected_chips=114),
            MockHashBoard(hashrate=37.0, temp=63.0, chips=114, expected_chips=114),
        ],
        fans=[MockFan(speed=4200), MockFan(speed=4100)],
    )


def _fw(enabled: bool = True) -> FirmwareConfig:
    return FirmwareConfig(enabled=enabled)


@pytest.fixture
def whatsminer_config() -> PluginConfig:
    return PluginConfig(
        plugin=PluginSettings(),
        miners={"whatsminer": MinerFamilyConfig(firmware={FW_STOCK: _fw()})},
    )


@pytest.fixture
def multi_family_config() -> PluginConfig:
    return PluginConfig(
        plugin=PluginSettings(),
        miners={
            "whatsminer": MinerFamilyConfig(firmware={FW_STOCK: _fw()}),
            "antminer": MinerFamilyConfig(firmware={FW_STOCK: _fw()}),
            "avalonminer": MinerFamilyConfig(firmware={FW_STOCK: _fw(False)}),
        },
    )


@pytest.fixture
def mock_secret() -> driver_pb2.SecretBundle:
    return driver_pb2.SecretBundle(
        version="1",
        user_pass=driver_pb2.UsernamePassword(username="admin", password="admin"),
    )


@pytest.fixture
def mock_ctx() -> MagicMock:
    ctx = MagicMock()
    ctx.abort = AsyncMock()
    return ctx
