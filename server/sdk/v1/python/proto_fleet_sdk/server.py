"""Plugin gRPC server implementation.

This module provides the PluginServer class for running a Python plugin as a gRPC server
that communicates with the Proto Fleet server.
"""

from __future__ import annotations

import asyncio
import logging
import signal
from typing import Any

import grpc

from proto_fleet_sdk.generated.pb import driver_pb2_grpc
from proto_fleet_sdk.protocols.driver import Driver
from proto_fleet_sdk.servicer import DriverServicer

__all__ = ["PluginServer"]

logger = logging.getLogger(__name__)

DEFAULT_PLUGIN_PORT = 50051


class PluginServer:
    """gRPC server for Python plugins.

    This class manages the lifecycle of a plugin gRPC server, including startup,
    graceful shutdown, and signal handling.
    """

    def __init__(self, driver: Driver, port: int = DEFAULT_PLUGIN_PORT, host: str = "localhost") -> None:
        self.driver = driver
        self.port = port
        self.host = host
        self.server: grpc.aio.Server | None = None
        self._shutdown_event = asyncio.Event()

    async def start(self) -> None:
        self.server = grpc.aio.server()

        # Add servicer
        servicer = DriverServicer(self.driver)
        driver_pb2_grpc.add_DriverServicer_to_server(servicer, self.server)

        # Bind to address
        address = f"{self.host}:{self.port}"
        actual_port = self.server.add_insecure_port(address)
        self.port = actual_port

        await self.server.start()
        logger.info("Plugin server started on %s:%s", self.host, self.port)

    async def stop(self, grace: float = 5.0) -> None:
        if self.server:
            logger.info("Stopping plugin server...")
            await self.server.stop(grace)
            self.server = None
            logger.info("Plugin server stopped")

    async def wait_for_termination(self) -> None:
        if self.server:
            await self.server.wait_for_termination()

    def _setup_signal_handlers(self) -> None:
        def signal_handler(signum: int, frame: Any) -> None:
            logger.info("Received signal %s, initiating graceful shutdown", signum)
            self._shutdown_event.set()

        signal.signal(signal.SIGTERM, signal_handler)
        signal.signal(signal.SIGINT, signal_handler)

    async def serve(self) -> None:
        """Start server and wait for termination.

        Example:
            >>> async def main():
            ...     driver = MyDriver()
            ...     server = PluginServer(driver, port=50051)
            ...     await server.serve()
            >>>
            >>> if __name__ == "__main__":
            ...     asyncio.run(main())
        """
        self._setup_signal_handlers()

        try:
            await self.start()

            # Wait for shutdown signal
            await self._shutdown_event.wait()

        except Exception as e:
            logger.error("Server error: %s", e, exc_info=True)
            raise
        finally:
            await self.stop()

    def run(self) -> None:
        """Synchronous entry point for running the server.

        Example:
            >>> if __name__ == "__main__":
            ...     driver = MyDriver()
            ...     server = PluginServer(driver, port=50051)
            ...     server.run()
        """
        asyncio.run(self.serve())
