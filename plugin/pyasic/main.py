#!/usr/bin/env python3
"""PyASIC Plugin for Proto Fleet.

Multi-manufacturer miner plugin built on PyASIC. Supports WhatsMiner, Avalon,
Goldshell, Auradine, BitAxe, IceRiver, and other miner families through
YAML-driven configuration with dynamic capability detection.
"""

import logging
import sys
from pathlib import Path

from proto_fleet_sdk.server import PluginServer

from pyasic_driver.config import load_config
from pyasic_driver.driver import PyAsicDriver


def _get_base_dir() -> Path:
    """Return the base directory for bundled resources.

    PyInstaller --onefile extracts to a temp dir exposed as sys._MEIPASS.
    When running from source, use the script's parent directory.
    """
    if getattr(sys, "frozen", False):
        return Path(sys._MEIPASS)  # type: ignore[attr-defined]  # noqa: SLF001
    return Path(__file__).parent


def _find_config() -> Path:
    """Find config.yaml, preferring a file beside the executable over the bundled one."""
    if getattr(sys, "frozen", False):
        beside_exe = Path(sys.executable).parent / "pyasic-config.yaml"
        if beside_exe.exists():
            return beside_exe
    return _get_base_dir() / "config.yaml"


def main() -> None:
    config_path = _find_config()
    if not config_path.exists():
        print(f"FATAL: config.yaml not found at {config_path}", file=sys.stderr)
        sys.exit(1)

    config = load_config(config_path)

    logging.basicConfig(
        level=getattr(logging, config.plugin.log_level.upper()),
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    )

    driver = PyAsicDriver(config)
    server = PluginServer(driver)
    server.run()


if __name__ == "__main__":
    main()
