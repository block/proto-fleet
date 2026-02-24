"""Tests for SDK utilities."""

from __future__ import annotations

import pytest

from proto_fleet_sdk.errors import InvalidConfigError
from proto_fleet_sdk.types import Capabilities
from proto_fleet_sdk.utils import (
    has_capability,
    hs_to_ths,
    jh_to_jth,
    jth_to_jh,
    parse_port,
    safe_index_conversion,
    safe_uint_to_int32,
    ths_to_hs,
)


class TestCapabilityHelpers:
    """Tests for capability helper functions."""

    def test_has_capability(self) -> None:
        """Test checking for capabilities."""
        caps: Capabilities = {"discover_device": True, "pair_device": False}
        assert has_capability(caps, "discover_device") is True
        assert has_capability(caps, "pair_device") is False
        assert has_capability(caps, "nonexistent") is False


class TestPortUtils:
    """Tests for port parsing utilities."""

    def test_parse_valid_port(self) -> None:
        """Test parsing valid port numbers."""
        assert parse_port("80") == 80
        assert parse_port("8080") == 8080
        assert parse_port("65535") == 65535
        assert parse_port("1") == 1

    def test_parse_invalid_port_string(self) -> None:
        """Test parsing invalid port string."""
        with pytest.raises(InvalidConfigError, match="must be a number"):
            parse_port("invalid")

    def test_parse_port_out_of_range(self) -> None:
        """Test parsing port out of range."""
        with pytest.raises(InvalidConfigError, match="must be between"):
            parse_port("0")

        with pytest.raises(InvalidConfigError, match="must be between"):
            parse_port("65536")


class TestTypeConverters:
    """Tests for type conversion utilities."""

    def test_safe_uint_to_int32_valid(self) -> None:
        """Test safe uint to int32 conversion with valid values."""
        assert safe_uint_to_int32(0) == 0
        assert safe_uint_to_int32(100) == 100
        assert safe_uint_to_int32(2147483647) == 2147483647  # Max int32

    def test_safe_uint_to_int32_negative(self) -> None:
        """Test safe uint to int32 conversion with negative value."""
        with pytest.raises(InvalidConfigError, match="cannot be negative"):
            safe_uint_to_int32(-1)

    def test_safe_uint_to_int32_overflow(self) -> None:
        """Test safe uint to int32 conversion with overflow."""
        with pytest.raises(InvalidConfigError, match="exceeds int32 maximum"):
            safe_uint_to_int32(2**31)

    def test_safe_index_conversion_valid(self) -> None:
        """Test safe index conversion with valid values."""
        assert safe_index_conversion(0) == 0
        assert safe_index_conversion(5) == 5
        assert safe_index_conversion(255) == 255

    def test_safe_index_conversion_negative(self) -> None:
        """Test safe index conversion with negative value."""
        with pytest.raises(InvalidConfigError, match="cannot be negative"):
            safe_index_conversion(-1)


class TestUnitConverters:
    """Tests for unit conversion utilities."""

    def test_ths_to_hs(self) -> None:
        """Test TH/s to H/s conversion."""
        assert ths_to_hs(110.0) == 110e12
        assert ths_to_hs(1.0) == 1e12
        assert ths_to_hs(0.0) == 0.0

    def test_jth_to_jh(self) -> None:
        """Test J/TH to J/H conversion."""
        assert jth_to_jh(29.5) == 29.5 / 1e12
        assert jth_to_jh(1e12) == 1.0
        assert jth_to_jh(0.0) == 0.0

    def test_hs_to_ths(self) -> None:
        """Test H/s to TH/s conversion."""
        assert hs_to_ths(110e12) == 110.0
        assert hs_to_ths(1e12) == 1.0
        assert hs_to_ths(0.0) == 0.0

    def test_jh_to_jth(self) -> None:
        """Test J/H to J/TH conversion."""
        assert abs(jh_to_jth(2.95e-11) - 29.5) < 0.0001
        assert jh_to_jth(1.0) == 1e12
        assert jh_to_jth(0.0) == 0.0

    def test_roundtrip_conversions(self) -> None:
        """Test that conversions are reversible."""
        # TH/s roundtrip
        original_ths = 110.0
        assert abs(hs_to_ths(ths_to_hs(original_ths)) - original_ths) < 0.0001

        # J/TH roundtrip
        original_jth = 29.5
        assert abs(jh_to_jth(jth_to_jh(original_jth)) - original_jth) < 0.0001
