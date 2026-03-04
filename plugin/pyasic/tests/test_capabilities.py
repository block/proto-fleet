"""Tests for dynamic capability builder."""

from __future__ import annotations

from unittest.mock import MagicMock

from pyasic.miners.base import BaseMiner

from pyasic_driver.capabilities import (
    FAMILY_TO_MAKE,
    MAKE_TO_FAMILY,
    STATIC_BASE_CAPABILITIES,
    _is_implemented,
    build_capabilities,
)


def _make_subclass_miner(*overrides: str, supports_power_modes: bool = False) -> BaseMiner:
    """Create a BaseMiner subclass that overrides specific methods for introspection testing."""
    overridden: dict[str, object] = {name: (lambda self: None) for name in overrides}
    overridden["make"] = "WhatsMiner"
    overridden["raw_model"] = "M60S"
    overridden["supports_shutdown"] = True
    overridden["supports_power_modes"] = supports_power_modes
    overridden["supports_presets"] = False
    overridden["supports_autotuning"] = False
    subclass = type("TestMiner", (BaseMiner,), overridden)
    return subclass("192.168.1.1")


class TestIsImplemented:
    def test_returns_false_for_missing_method(self) -> None:
        # Arrange
        miner = MagicMock(spec=[])

        # Act
        result = _is_implemented(miner, "nonexistent_method")

        # Assert
        assert result is False

    def test_returns_false_for_base_method(self) -> None:
        # Arrange — no overrides, so reboot is the base no-op
        miner = _make_subclass_miner()

        # Act & Assert
        assert _is_implemented(miner, "reboot") is False

    def test_returns_true_for_overridden_method(self) -> None:
        # Arrange — reboot is overridden in the subclass
        miner = _make_subclass_miner("reboot")

        # Act & Assert
        assert _is_implemented(miner, "reboot") is True


class TestBuildCapabilities:
    def test_static_telemetry_caps_always_true(self) -> None:
        # Arrange — base miner with no overrides still has telemetry caps
        miner = _make_subclass_miner()

        # Act
        caps = build_capabilities(miner)

        # Assert
        assert caps["device_status"] is True
        assert caps["hashrate_reported"] is True
        assert caps["power_usage_reported"] is True
        assert caps["temperature_reported"] is True
        assert caps["fan_speed_reported"] is True
        assert caps["efficiency_reported"] is True
        assert caps["per_board_stats"] is True
        assert caps["psu_stats_reported"] is True

    def test_control_caps_detected_from_overrides(self) -> None:
        # Arrange — override control methods
        miner = _make_subclass_miner("reboot", "fault_light_on", "resume_mining", "stop_mining")

        # Act
        caps = build_capabilities(miner)

        # Assert
        assert caps["reboot"] is True
        assert caps["led_blink"] is True
        assert caps["mining_start"] is True
        assert caps["mining_stop"] is True

    def test_control_caps_false_when_not_overridden(self) -> None:
        # Arrange — no overrides
        miner = _make_subclass_miner()

        # Act
        caps = build_capabilities(miner)

        # Assert
        assert caps["reboot"] is False
        assert caps["led_blink"] is False
        assert caps["mining_start"] is False
        assert caps["mining_stop"] is False

    def test_power_modes_from_support_flag(self) -> None:
        # Arrange
        miner_with = _make_subclass_miner()
        miner_with.supports_power_modes = True
        miner_without = _make_subclass_miner()
        miner_without.supports_power_modes = False

        # Act
        caps_with = build_capabilities(miner_with)
        caps_without = build_capabilities(miner_without)

        # Assert
        assert caps_with["power_mode_efficiency"] is True
        assert caps_without["power_mode_efficiency"] is False

    def test_unsupported_caps_always_false(self) -> None:
        # Arrange
        miner = _make_subclass_miner()

        # Act
        caps = build_capabilities(miner)

        # Assert
        assert caps["set_cooling_mode"] is False
        assert caps["get_cooling_mode"] is False
        assert caps["update_miner_password"] is False
        assert caps["streaming"] is False
        assert caps["batch_status"] is False


class TestFamilyMappings:
    def test_family_to_make_has_all_expected_families(self) -> None:
        expected = {
            "whatsminer", "antminer", "avalonminer", "goldshell", "auradine",
            "bitaxe", "iceriver", "innosilicon", "braiins", "epic", "hammer",
            "volcminer", "elphapex", "luckyminer",
        }

        # Act & Assert
        assert set(FAMILY_TO_MAKE.keys()) == expected

    def test_make_to_family_is_inverse(self) -> None:
        for family, make in FAMILY_TO_MAKE.items():
            assert MAKE_TO_FAMILY[make] == family

    def test_static_base_capabilities_has_core_caps(self) -> None:
        assert STATIC_BASE_CAPABILITIES["discovery"] is True
        assert STATIC_BASE_CAPABILITIES["pairing"] is True
        assert STATIC_BASE_CAPABILITIES["device_status"] is True
