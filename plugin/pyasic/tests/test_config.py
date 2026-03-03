"""Tests for config loading and validation."""

from __future__ import annotations

from pathlib import Path
from textwrap import dedent

import pytest
from proto_fleet_sdk.errors import InvalidConfigError

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
                enabled: true
        """)

        # Act
        config = load_config(path)

        # Assert
        assert config.plugin.log_level == "debug"
        assert config.plugin.discovery_timeout_seconds == 15
        assert config.plugin.telemetry_cache_ttl_seconds == 10
        assert config.miners["whatsminer"].enabled is True

    def test_minimal_config(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            miners:
              whatsminer:
                enabled: true
        """)

        # Act
        config = load_config(path)

        # Assert — defaults applied
        assert config.plugin.log_level == "info"
        assert config.plugin.discovery_timeout_seconds == 10
        assert config.miners["whatsminer"].enabled is True

    def test_no_enabled_families_raises(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            miners:
              whatsminer:
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
                enabled: true
              unknown_miner:
                enabled: true
        """)

        # Act
        config = load_config(path)

        # Assert
        assert "unknown_miner" not in config.miners
        assert "whatsminer" in config.miners

    def test_enabled_makes(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            miners:
              whatsminer:
                enabled: true
              antminer:
                enabled: true
              goldshell:
                enabled: false
        """)

        # Act
        config = load_config(path)
        makes = config.enabled_makes()

        # Assert
        assert makes == {"WhatsMiner", "AntMiner"}

    def test_multi_family_config(self, tmp_config) -> None:
        # Arrange
        path = tmp_config("""\
            miners:
              whatsminer:
                enabled: true
              auradine:
                enabled: true
              iceriver:
                enabled: false
        """)

        # Act
        config = load_config(path)

        # Assert
        assert config.miners["whatsminer"].enabled is True
        assert config.miners["auradine"].enabled is True
        assert config.miners["iceriver"].enabled is False
