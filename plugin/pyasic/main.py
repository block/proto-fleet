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


def main() -> None:
    config_path = Path(__file__).parent / "config.yaml"
    if not config_path.exists():
        print(f"FATAL: config.yaml not found at {config_path}", file=sys.stderr)
        sys.exit(1)

    config = load_config(config_path)

    logging.basicConfig(
        level=getattr(logging, config.plugin.log_level.upper()),
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    )

    driver = PyAsicDriver(config)
    server = PluginServer(driver, port=0, host="localhost")
    server.run()


if __name__ == "__main__":
    main()
