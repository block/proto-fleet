"""YAML config loading and validation for the PyASIC plugin."""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import yaml
from proto_fleet_sdk.errors import InvalidConfigError

from pyasic_driver.capabilities import FAMILY_TO_MAKE

logger = logging.getLogger(__name__)

_VALID_LOG_LEVELS = {"debug", "info", "warn", "warning", "error"}

DEFAULT_DISCOVERY_TIMEOUT_SECONDS = 10
DEFAULT_TELEMETRY_CACHE_TTL_SECONDS = 5


@dataclass(frozen=True)
class PluginSettings:
    log_level: str = "info"
    discovery_timeout_seconds: int = DEFAULT_DISCOVERY_TIMEOUT_SECONDS
    telemetry_cache_ttl_seconds: int = DEFAULT_TELEMETRY_CACHE_TTL_SECONDS


@dataclass(frozen=True)
class MinerFamilyConfig:
    enabled: bool = False


@dataclass(frozen=True)
class PluginConfig:
    plugin: PluginSettings = field(default_factory=PluginSettings)
    miners: dict[str, MinerFamilyConfig] = field(default_factory=dict)

    def enabled_makes(self) -> set[str]:
        """Return pyasic make strings for all enabled families."""
        makes: set[str] = set()
        for family_name, family_config in self.miners.items():
            if family_config.enabled and family_name in FAMILY_TO_MAKE:
                makes.add(FAMILY_TO_MAKE[family_name])
        return makes


def load_config(path: Path) -> PluginConfig:
    """Load and validate plugin configuration from a YAML file."""
    with open(path) as f:
        raw = yaml.safe_load(f)

    if raw is None:
        raise InvalidConfigError(f"Config file is empty: {path}")
    if not isinstance(raw, dict):
        raise InvalidConfigError(f"Config must be a YAML mapping, got {type(raw).__name__}")

    plugin_settings = _parse_plugin_settings(raw.get("plugin", {}))
    miners = _parse_miners(raw.get("miners", {}))

    enabled_count = sum(1 for m in miners.values() if m.enabled)
    if enabled_count == 0:
        raise InvalidConfigError("At least one miner family must be enabled")

    return PluginConfig(plugin=plugin_settings, miners=miners)


def _parse_plugin_settings(raw: Any) -> PluginSettings:
    if not isinstance(raw, dict):
        return PluginSettings()

    log_level = raw.get("log_level", "info")
    if isinstance(log_level, str) and log_level.lower() not in _VALID_LOG_LEVELS:
        logger.warning("Unknown log_level '%s', defaulting to 'info'", log_level)
        log_level = "info"

    discovery_timeout = raw.get("discovery_timeout_seconds", DEFAULT_DISCOVERY_TIMEOUT_SECONDS)
    cache_ttl = raw.get("telemetry_cache_ttl_seconds", DEFAULT_TELEMETRY_CACHE_TTL_SECONDS)

    return PluginSettings(
        log_level=str(log_level),
        discovery_timeout_seconds=int(discovery_timeout),
        telemetry_cache_ttl_seconds=int(cache_ttl),
    )


def _parse_miners(raw: Any) -> dict[str, MinerFamilyConfig]:
    if not isinstance(raw, dict):
        return {}

    miners: dict[str, MinerFamilyConfig] = {}
    for family_name, family_raw in raw.items():
        if family_name not in FAMILY_TO_MAKE:
            logger.warning("Unknown miner family '%s', skipping", family_name)
            continue

        if not isinstance(family_raw, dict):
            miners[family_name] = MinerFamilyConfig()
            continue

        enabled = family_raw.get("enabled", False)
        miners[family_name] = MinerFamilyConfig(enabled=bool(enabled))

    return miners
