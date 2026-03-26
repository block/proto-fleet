"""Plugin gRPC server implementation.

This module provides the PluginServer class for running a Python plugin as a gRPC server
that communicates with the Proto Fleet server via the HashiCorp go-plugin protocol.

The go-plugin protocol requires:
  1. Magic cookie environment variable check for security
  2. Connection info printed to stdout after server starts:
     ``CORE_PROTOCOL_VERSION|APP_PROTOCOL_VERSION|NETWORK_TYPE|NETWORK_ADDR|PROTOCOL``
"""

from __future__ import annotations

import asyncio
import logging
import os
import signal
import sys
from typing import Any

import grpc

from proto_fleet_sdk.generated.pb import driver_pb2_grpc

__all__ = ["PluginServer"]

logger = logging.getLogger(__name__)

DEFAULT_PLUGIN_PORT = 0

# go-plugin protocol constants — must match server/sdk/v1/plugin.go HandshakeConfig
_MAGIC_COOKIE_KEY = "MINER_DRIVER_PLUGIN"
_MAGIC_COOKIE_VALUE = "fleet-miner-driver"
_CORE_PROTOCOL_VERSION = 1
_APP_PROTOCOL_VERSION = 1


class PluginServer:
    """gRPC server for Python plugins.

    Implements the HashiCorp go-plugin protocol so the Go server can launch
    this plugin as a subprocess, negotiate a gRPC connection, and communicate
    over the SDK's Driver service.

    The servicer must be an instance of driver_pb2_grpc.DriverServicer (the
    protoc-generated base class). Plugin authors implement the gRPC servicer
    directly with proto types and use the @grpc_error_handler decorator from
    proto_fleet_sdk.errors for automatic SDK error → gRPC status mapping.
    """

    def __init__(
        self, servicer: driver_pb2_grpc.DriverServicer, port: int = DEFAULT_PLUGIN_PORT, host: str = "127.0.0.1"
    ) -> None:
        self.servicer = servicer
        self.port = port
        self.host = host
        self.server: grpc.aio.Server | None = None
        self._shutdown_event = asyncio.Event()

    @staticmethod
    def _check_magic_cookie() -> None:
        """Verify the go-plugin magic cookie.

        go-plugin sets this env var when launching the subprocess. If it's
        missing or wrong, we're not being launched by the host — exit early.
        """
        actual = os.environ.get(_MAGIC_COOKIE_KEY)
        if actual != _MAGIC_COOKIE_VALUE:
            print(
                f"This binary is a plugin. It is not meant to be executed directly. "
                f"Expected {_MAGIC_COOKIE_KEY}={_MAGIC_COOKIE_VALUE}",
                file=sys.stderr,
            )
            sys.exit(1)

    def _emit_go_plugin_handshake(self) -> None:
        """Print the go-plugin connection advertisement to stdout.

        Format: CORE_PROTOCOL_VERSION|APP_PROTOCOL_VERSION|NETWORK_TYPE|NETWORK_ADDR|PROTOCOL
        """
        line = f"{_CORE_PROTOCOL_VERSION}|{_APP_PROTOCOL_VERSION}|tcp|{self.host}:{self.port}|grpc"
        sys.stdout.write(line + "\n")
        sys.stdout.flush()

    async def start(self) -> None:
        self.server = grpc.aio.server()

        # Register the servicer directly
        driver_pb2_grpc.add_DriverServicer_to_server(self.servicer, self.server)  # type: ignore[no-untyped-call]  # generated gRPC code lacks type stubs

        # Bind to address (port=0 lets the OS pick a free port)
        address = f"{self.host}:{self.port}"
        actual_port = self.server.add_insecure_port(address)
        if actual_port == 0:
            raise RuntimeError(f"Failed to bind to {address}")
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

    @staticmethod
    def _asyncio_exception_handler(loop: asyncio.AbstractEventLoop, context: dict[str, Any]) -> None:
        """Global handler for unhandled exceptions in fire-and-forget tasks."""
        exception = context.get("exception")
        message = context.get("message", "Unhandled exception in async task")
        if exception:
            logger.error("%s: %s", message, exception, exc_info=exception)
        else:
            logger.error("%s", message)

    async def serve(self) -> None:
        """Start server and wait for termination.

        Example:
            >>> async def main():
            ...     servicer = MyDriverServicer()
            ...     server = PluginServer(servicer)
            ...     await server.serve()
            >>>
            >>> if __name__ == "__main__":
            ...     asyncio.run(main())
        """
        self._check_magic_cookie()
        self._setup_signal_handlers()

        loop = asyncio.get_running_loop()
        loop.set_exception_handler(self._asyncio_exception_handler)

        try:
            await self.start()

            # Emit handshake after successful start so go-plugin host knows where to connect
            self._emit_go_plugin_handshake()

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
            ...     servicer = MyDriverServicer()
            ...     server = PluginServer(servicer)
            ...     server.run()
        """
        asyncio.run(self.serve())
