"""Tests for SDK enums."""

from __future__ import annotations

from proto_fleet_sdk.enums import HealthStatus


class TestHealthStatus:
    """Tests for HealthStatus enum."""

    def test_unknown_value_forward_compatibility(self) -> None:
        """Test that unknown proto values map to HEALTH_UNKNOWN for forward compatibility."""
        assert HealthStatus(999) == HealthStatus.HEALTH_UNKNOWN
