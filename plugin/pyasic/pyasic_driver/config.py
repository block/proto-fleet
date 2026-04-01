"""YAML config loading and validation for the PyASIC plugin."""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import yaml
from proto_fleet_sdk.errors import InvalidConfigError

from pyasic_driver.capabilities import FAMILY_TO_MAKE, FIRMWARE_VARIANTS

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
class FirmwareConfig:
    enabled: bool = False


@dataclass(frozen=True)
class MinerFamilyConfig:
    firmware: dict[str, FirmwareConfig] = field(default_factory=dict)

    @property
    def is_enabled(self) -> bool:
        return any(fw.enabled for fw in self.firmware.values())


@dataclass(frozen=True)
class PluginConfig:
    plugin: PluginSettings = field(default_factory=PluginSettings)
    miners: dict[str, MinerFamilyConfig] = field(default_factory=dict)

    def enabled_firmware(self, family: str) -> set[str]:
        cfg = self.miners.get(family)
        if not cfg:
            return set()
        return {name for name, fw in cfg.firmware.items() if fw.enabled}


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

        firmware = _parse_firmware(family_name, family_raw)
        miners[family_name] = MinerFamilyConfig(firmware=firmware)

    return miners


def _parse_firmware(family_name: str, raw: dict[str, Any]) -> dict[str, FirmwareConfig]:
    known_variants = FIRMWARE_VARIANTS.get(family_name, {})
    firmware: dict[str, FirmwareConfig] = {}
    for variant_name, variant_raw in raw.items():
        if variant_name not in known_variants:
            logger.warning(
                "Unknown firmware variant '%s' for family '%s', skipping",
                variant_name, family_name,
            )
            continue
        if not isinstance(variant_raw, dict):
            firmware[variant_name] = FirmwareConfig()
            continue
        firmware[variant_name] = FirmwareConfig(enabled=bool(variant_raw.get("enabled", False)))
    return firmware
