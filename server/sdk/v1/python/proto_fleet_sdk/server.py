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
import hashlib
import inspect
import logging
import os
import secrets
import signal
import sys
from collections.abc import AsyncIterator
from contextlib import AsyncExitStack, asynccontextmanager
from typing import Any

import grpc
from google.protobuf.any_pb2 import Any as ProtoAny
from google.rpc import (  # type: ignore[import-untyped]  # Canonical generated protos ship without typing metadata.
    error_details_pb2,
    status_pb2,
)

from proto_fleet_sdk.generated.pb import driver_pb2, driver_pb2_grpc

__all__ = ["PluginServer"]

logger = logging.getLogger(__name__)

DEFAULT_PLUGIN_PORT = 0

# go-plugin protocol constants — must match server/sdk/v1/plugin.go HandshakeConfig
_MAGIC_COOKIE_KEY = "MINER_DRIVER_PLUGIN"
_MAGIC_COOKIE_VALUE = "fleet-miner-driver"
_CORE_PROTOCOL_VERSION = 1
_APP_PROTOCOL_VERSION = 1


class _SynchronousLifecycleContext:
    """Keep blocking author lifecycle methods' gRPC context semantics."""

    def __init__(self, context: Any, loop: asyncio.AbstractEventLoop) -> None:
        self._context = context
        self._loop = loop

    def __getattr__(self, name: str) -> Any:
        return getattr(self._context, name)

    def abort(self, code: Any, details: str, trailing_metadata: Any = ()) -> None:
        asyncio.run_coroutine_threadsafe(
            self._context.abort(code, details, trailing_metadata), self._loop
        ).result()

    def abort_with_status(self, status: Any) -> None:
        self.abort(status.code, status.details, status.trailing_metadata or ())

    def is_active(self) -> bool:
        return not self._context.done()

    def add_callback(self, callback: Any) -> bool:
        async def register() -> bool:
            if self._context.done():
                return False
            self._context.add_done_callback(lambda _context: callback())
            return True

        return asyncio.run_coroutine_threadsafe(register(), self._loop).result()


class _ScopedDriverServicer:
    """Reuse only an identical opted-in request in the author's active registry.

    Authors own the device objects and keep them valid until CloseDevice or
    explicit NewDevice replacement. The SDK retains request digests and registration tokens.
    Other unary and streaming methods delegate without changing their shape.
    """

    def __init__(self, servicer: driver_pb2_grpc.DriverServicer) -> None:
        self._servicer = servicer
        self._reuse_keys: dict[str, bytes] = {}
        self._closing: set[str] = set()
        self._locks: dict[str, tuple[asyncio.Lock, int]] = {}
        self._generations: dict[str, bytes] = {}

    def __getattr__(self, name: str) -> Any:
        method = getattr(self._servicer, name)
        # Stream handlers retain their iterator shape. Every unary device call
        # runs under the same ID lock as author registry replacement.
        descriptor = driver_pb2.DESCRIPTOR.services_by_name["Driver"].methods_by_name.get(name)
        if descriptor is None or descriptor.server_streaming:
            return method

        async def guarded(request: Any, context: Any) -> Any:
            refs: list[Any] = []
            if isinstance(request, (driver_pb2.DeviceRef, driver_pb2.DescribeDeviceRequest)):
                refs = [request]
            elif hasattr(request, "ref"):
                refs = [request.ref]
            elif hasattr(request, "refs"):
                refs = list(request.refs)
            async with AsyncExitStack() as stack:
                for identifier in sorted({ref.device_id for ref in refs}):
                    await stack.enter_async_context(self._lock(identifier))
                for ref in refs:
                    await self._check_generation(ref, context)
                return await self._invoke(method, request, context)

        return guarded

    @asynccontextmanager
    async def _lock(self, identifier: str) -> AsyncIterator[None]:
        lock, refs = self._locks.get(identifier, (asyncio.Lock(), 0))
        self._locks[identifier] = (lock, refs + 1)
        try:
            async with lock:
                yield
        finally:
            _, refs = self._locks[identifier]
            if refs == 1:
                del self._locks[identifier]
            else:
                self._locks[identifier] = (lock, refs - 1)

    async def _check_generation(
        self, request: Any, context: Any, *, allow_closing: bool = False
    ) -> None:
        if request.backend_generation != self._generations.get(request.device_id, b"") or (
            request.device_id in self._closing and not allow_closing
        ):
            detail = ProtoAny()
            detail.Pack(
                error_details_pb2.ErrorInfo(
                    reason="SDK_BACKEND_GENERATION_MISMATCH", domain="protofleet.sdk"
                )
            )
            rejection = status_pb2.Status(
                code=9, message="SDK backend registration changed", details=[detail]
            )
            await context.abort(
                grpc.StatusCode.FAILED_PRECONDITION,
                rejection.message,
                trailing_metadata=(("grpc-status-details-bin", rejection.SerializeToString()),),
            )

    @staticmethod
    async def _invoke(method: Any, request: Any, context: Any) -> Any:
        if inspect.iscoroutinefunction(method):
            return await method(request, context)
        # Canceling to_thread does not stop the author's synchronous method.
        # Keep its ID lock until the real registry mutation has finished.
        synchronous_context = _SynchronousLifecycleContext(context, asyncio.get_running_loop())
        task = asyncio.create_task(asyncio.to_thread(method, request, synchronous_context))
        canceled = False
        while not task.done():
            try:
                await asyncio.shield(task)
            except asyncio.CancelledError:
                canceled = True
            except Exception:
                if canceled:
                    raise asyncio.CancelledError from None
                raise
        if canceled:
            # Retrieve any worker failure without replacing the cancellation.
            if not task.cancelled():
                task.exception()
            raise asyncio.CancelledError
        result = task.result()
        return await result if inspect.isawaitable(result) else result

    async def DescribeDriver(self, request: Any, context: Any) -> Any:
        original = await self._invoke(self._servicer.DescribeDriver, request, context)
        response = driver_pb2.DescribeDriverResponse()
        response.CopyFrom(original)
        response.caps.flags["sdk_scoped_device_reuse"] = True
        return response

    async def NewDevice(self, request: Any, context: Any) -> Any:
        identifier = request.device_id
        digest = hashlib.sha256(request.SerializeToString(deterministic=True)).digest()
        async with self._lock(identifier):
            if context.done():
                await context.abort(grpc.StatusCode.CANCELLED, "device resolution canceled")
            if identifier in self._closing:
                await context.abort(grpc.StatusCode.UNAVAILABLE, "device cleanup incomplete")
            if request.reuse_scope_key and self._reuse_keys.get(identifier) == digest:
                return driver_pb2.NewDeviceResponse(
                    device_id=identifier,
                    reuse_scope_key=request.reuse_scope_key,
                    backend_generation=self._generations[identifier],
                )
            # Failed and default creation discard evidence for the former request.
            self._reuse_keys.pop(identifier, None)
            self._generations[identifier] = (
                secrets.token_bytes(32) if request.reuse_scope_key else b""
            )
            original = await self._invoke(self._servicer.NewDevice, request, context)
            if context.done():
                await context.abort(grpc.StatusCode.CANCELLED, "device resolution canceled")
            if original.device_id != identifier:
                await context.abort(grpc.StatusCode.INTERNAL, "device ID mismatch")
            response = driver_pb2.NewDeviceResponse()
            response.CopyFrom(original)
            response.reuse_scope_key = request.reuse_scope_key
            response.backend_generation = self._generations[identifier]
            if request.reuse_scope_key:
                self._reuse_keys[identifier] = digest
            return response

    async def CloseDevice(self, request: Any, context: Any) -> Any:
        identifier = request.device_id
        async with self._lock(identifier):
            await self._check_generation(request, context, allow_closing=True)
            self._reuse_keys.pop(identifier, None)
            self._closing.add(identifier)
            try:
                result = await self._invoke(self._servicer.CloseDevice, request, context)
            except BaseException:
                if context.code() == grpc.StatusCode.NOT_FOUND:
                    self._closing.discard(identifier)
                    self._generations.pop(identifier, None)
                raise
            self._closing.discard(identifier)
            self._generations.pop(identifier, None)
            return result


class PluginServer:
    """gRPC server for Python plugins.

    Implements the HashiCorp go-plugin protocol so the Go server can launch
    this plugin as a subprocess, negotiate a gRPC connection, and communicate
    over the SDK's Driver service.

    The servicer must be an instance of driver_pb2_grpc.DriverServicer (the
    protoc-generated base class). Plugin authors implement the gRPC servicer
    directly with proto types and use the @grpc_error_handler decorator from
    proto_fleet_sdk.errors for automatic SDK error → gRPC status mapping.

    Scoped NewDevice calls reuse an identical complete request in the author's
    registry. Authors keep those objects alive until CloseDevice or explicit
    replacement; the SDK retains request digests, not a second object cache.
    Empty reuse_scope_key preserves unconditional author NewDevice calls.
    """

    def __init__(
        self,
        servicer: driver_pb2_grpc.DriverServicer,
        port: int = DEFAULT_PLUGIN_PORT,
        host: str = "127.0.0.1",
    ) -> None:
        self.servicer = servicer
        self._scoped_servicer = _ScopedDriverServicer(servicer)
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

        # Lifecycle and device unary calls pass through SDK registration guards.
        driver_pb2_grpc.add_DriverServicer_to_server(self._scoped_servicer, self.server)  # type: ignore[no-untyped-call]  # generated gRPC code lacks type stubs

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
    def _asyncio_exception_handler(
        loop: asyncio.AbstractEventLoop, context: dict[str, Any]
    ) -> None:
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
