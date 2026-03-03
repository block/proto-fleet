"""Tests for core SDK types."""

from __future__ import annotations

import pytest

from proto_fleet_sdk.errors import InvalidConfigError
from proto_fleet_sdk.types import (
    Capabilities,
    ConfiguredPool,
    DeviceInfo,
    DriverIdentifier,
    MiningPoolConfig,
)


class TestDriverIdentifier:
    """Tests for DriverIdentifier type."""

    def test_create_valid_identifier(self) -> None:
        """Test creating a valid driver identifier."""
        ident = DriverIdentifier(driver_name="test-plugin", api_version="v1")
        assert ident.driver_name == "test-plugin"
        assert ident.api_version == "v1"

    def test_empty_driver_name_raises(self) -> None:
        """Test that empty driver name raises InvalidConfigError."""
        with pytest.raises(InvalidConfigError, match="driver_name cannot be empty"):
            DriverIdentifier(driver_name="", api_version="v1")

    def test_empty_api_version_raises(self) -> None:
        """Test that empty API version raises InvalidConfigError."""
        with pytest.raises(InvalidConfigError, match="api_version cannot be empty"):
            DriverIdentifier(driver_name="test", api_version="")


class TestCapabilities:
    """Tests for Capabilities type."""

    def test_create_capabilities(self) -> None:
        """Test creating capabilities."""
        caps: Capabilities = {"discover_device": True, "pair_device": False}
        assert caps == {"discover_device": True, "pair_device": False}


class TestDeviceInfo:
    """Tests for DeviceInfo type."""

    def test_create_valid_device_info(self) -> None:
        """Test creating valid device info."""
        info = DeviceInfo(
            host="192.168.1.100",
            port=80,
            url_scheme="http",
            serial_number="SN123",
            model="Test Miner",
            manufacturer="Test Corp",
            mac_address="00:1A:2B:3C:4D:5E",
            firmware_version="1.0.0",
        )
        assert info.host == "192.168.1.100"
        assert info.port == 80

    def test_empty_host_raises(self) -> None:
        """Test that empty host raises InvalidConfigError."""
        with pytest.raises(InvalidConfigError, match="host cannot be empty"):
            DeviceInfo(
                host="",
                port=80,
                url_scheme="http",
                serial_number="SN123",
                model="Test",
                manufacturer="Test",
                mac_address="00:00:00:00:00:00",
                firmware_version="1.0.0",
            )

    def test_invalid_port_raises(self) -> None:
        """Test that invalid port raises InvalidConfigError."""
        with pytest.raises(InvalidConfigError, match="port must be between"):
            DeviceInfo(
                host="192.168.1.100",
                port=0,
                url_scheme="http",
                serial_number="SN123",
                model="Test",
                manufacturer="Test",
                mac_address="00:00:00:00:00:00",
                firmware_version="1.0.0",
            )

    def test_empty_url_scheme_raises(self) -> None:
        with pytest.raises(InvalidConfigError, match="url_scheme cannot be empty"):
            DeviceInfo(
                host="192.168.1.100",
                port=80,
                url_scheme="",
                serial_number="SN123",
                model="Test",
                manufacturer="Test",
                mac_address="00:00:00:00:00:00",
                firmware_version="1.0.0",
            )

    def test_invalid_url_scheme_raises(self) -> None:
        with pytest.raises(InvalidConfigError, match="valid URI scheme"):
            DeviceInfo(
                host="192.168.1.100",
                port=80,
                url_scheme="123-bad",
                serial_number="SN123",
                model="Test",
                manufacturer="Test",
                mac_address="00:00:00:00:00:00",
                firmware_version="1.0.0",
            )


class TestMiningPoolConfig:
    """Tests for MiningPoolConfig type."""

    def test_create_valid_pool_config(self) -> None:
        """Test creating valid pool configuration."""
        pool = MiningPoolConfig(
            priority=0, url="stratum+tcp://pool.example:3333", worker_name="worker1"
        )
        assert pool.priority == 0
        assert pool.url == "stratum+tcp://pool.example:3333"
        assert pool.worker_name == "worker1"

    def test_negative_priority_raises(self) -> None:
        """Test that negative priority raises InvalidConfigError."""
        with pytest.raises(InvalidConfigError, match="priority must be non-negative"):
            MiningPoolConfig(priority=-1, url="stratum+tcp://pool.example:3333", worker_name="w1")

    def test_empty_url_raises(self) -> None:
        """Test that empty URL raises InvalidConfigError."""
        with pytest.raises(InvalidConfigError, match="url cannot be empty"):
            MiningPoolConfig(priority=0, url="", worker_name="worker1")

    def test_empty_worker_name_raises(self) -> None:
        with pytest.raises(InvalidConfigError, match="worker_name cannot be empty"):
            MiningPoolConfig(priority=0, url="stratum+tcp://pool.example:3333", worker_name="")


class TestConfiguredPool:
    """Tests for ConfiguredPool type."""

    def test_create_configured_pool(self) -> None:
        """Test creating configured pool."""
        pool = ConfiguredPool(
            priority=0, url="stratum+tcp://pool.example:3333", username="worker1"
        )
        assert pool.priority == 0
        assert pool.url == "stratum+tcp://pool.example:3333"
        assert pool.username == "worker1"
