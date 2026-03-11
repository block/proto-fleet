"""Tests for config loading and validation."""

from __future__ import annotations

from pathlib import Path
from textwrap import dedent

import pytest
from proto_fleet_sdk.errors import InvalidConfigError

from pyasic_driver.capabilities import FW_BRAIINS, FW_STOCK, FW_VNISH
from pyasic_driver.config import load_config


@pytest.fixture
def tmp_config(tmp_path: Path):
    """Helper to write a config YAML and return its path."""
    def _write(content: str) -> Path:
        p = tmp_path / "config.yaml"
        p.write_text(dedent(content))
        return p
    return _write


class TestLoadConfig:
    def test_valid_config(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            plugin:
              log_level: debug
              discovery_timeout_seconds: 15
              telemetry_cache_ttl_seconds: 10
            miners:
              whatsminer:
                stock:
                  enabled: true
        """)

        # Act
        config = load_config(path)

        # Assert
        assert config.plugin.log_level == "debug"
        assert config.plugin.discovery_timeout_seconds == 15
        assert config.plugin.telemetry_cache_ttl_seconds == 10
        assert config.miners["whatsminer"].is_enabled is True

    def test_minimal_config(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            miners:
              whatsminer:
                stock:
                  enabled: true
        """)

        # Act
        config = load_config(path)

        # Assert — defaults applied
        assert config.plugin.log_level == "info"
        assert config.plugin.discovery_timeout_seconds == 10
        assert config.miners["whatsminer"].is_enabled is True

    def test_no_enabled_families_raises(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            miners:
              whatsminer:
                stock:
                  enabled: false
        """)

        # Act & Assert
        with pytest.raises(InvalidConfigError, match="At least one miner family must be enabled"):
            load_config(path)

    def test_empty_config_raises(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("")

        # Act & Assert
        with pytest.raises(InvalidConfigError, match="empty"):
            load_config(path)

    def test_unknown_family_skipped(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            miners:
              whatsminer:
                stock:
                  enabled: true
              unknown_miner:
                stock:
                  enabled: true
        """)

        # Act
        config = load_config(path)

        # Assert
        assert "unknown_miner" not in config.miners
        assert "whatsminer" in config.miners

    def test_multi_family_config(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            miners:
              whatsminer:
                stock:
                  enabled: true
              auradine:
                stock:
                  enabled: true
              iceriver:
                stock:
                  enabled: false
        """)

        # Act
        config = load_config(path)

        # Assert
        assert config.miners["whatsminer"].is_enabled is True
        assert config.miners["auradine"].is_enabled is True
        assert config.miners["iceriver"].is_enabled is False

    def test_firmware_variants_parsed(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            miners:
              antminer:
                stock:
                  enabled: false
                braiins:
                  enabled: true
                vnish:
                  enabled: false
        """)

        # Act
        config = load_config(path)

        # Assert
        fw = config.miners["antminer"].firmware
        assert fw[FW_STOCK].enabled is False
        assert fw[FW_BRAIINS].enabled is True
        assert fw[FW_VNISH].enabled is False
        assert config.miners["antminer"].is_enabled is True

    def test_all_variants_disabled_not_enabled(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            miners:
              whatsminer:
                stock:
                  enabled: true
              antminer:
                stock:
                  enabled: false
                braiins:
                  enabled: false
        """)

        # Act
        config = load_config(path)

        # Assert
        assert config.miners["antminer"].is_enabled is False

    def test_enabled_firmware_returns_enabled_variants(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            miners:
              antminer:
                stock:
                  enabled: false
                braiins:
                  enabled: true
                vnish:
                  enabled: true
        """)

        # Act
        config = load_config(path)

        # Assert
        assert config.enabled_firmware("antminer") == {FW_BRAIINS, FW_VNISH}

    def test_unknown_firmware_variant_skipped(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            miners:
              whatsminer:
                stock:
                  enabled: true
                unknown_fw:
                  enabled: true
        """)

        # Act
        config = load_config(path)

        # Assert
        assert "unknown_fw" not in config.miners["whatsminer"].firmware
