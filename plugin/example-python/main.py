#!/usr/bin/env python3
"""Example Python Plugin for Proto Fleet.

Minimal example demonstrating how to build a Proto Fleet plugin in Python.
Use this as a starting point for your own plugin — replace ExampleDriver
with your device-specific driver implementation.
"""

import logging

from proto_fleet_sdk.server import PluginServer

from example_driver.driver import ExampleDriver


def main() -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    )

    driver = ExampleDriver()
    server = PluginServer(driver)
    server.run()


if __name__ == "__main__":
    main()
