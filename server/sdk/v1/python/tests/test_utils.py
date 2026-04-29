"""Tests for SDK utilities."""

from __future__ import annotations

import pytest

from proto_fleet_sdk import (
    CAP_CURTAIL_FULL,
    CAP_CURTAIL_EFFICIENCY,
    CAP_CURTAIL_PARTIAL,
)
from proto_fleet_sdk.errors import InvalidConfigError
from proto_fleet_sdk.utils import (
    has_capability,
    parse_port,
)


class TestCapabilityHelpers:
    """Tests for capability helper functions."""

    def test_has_capability(self) -> None:
        """Test checking for capabilities."""
        caps: dict[str, bool] = {"discover_device": True, "pair_device": False}
        assert has_capability(caps, "discover_device") is True
        assert has_capability(caps, "pair_device") is False
        assert has_capability(caps, "nonexistent") is False

    def test_curtail_capability_constants_are_exported(self) -> None:
        """Test curtailment capability constants."""
        assert CAP_CURTAIL_FULL == "curtail_full"
        assert CAP_CURTAIL_EFFICIENCY == "curtail_efficiency"
        assert CAP_CURTAIL_PARTIAL == "curtail_partial"


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
