"""Exercise SDK registry ownership through actual grpc.aio calls."""

import asyncio
import threading
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from dataclasses import dataclass
from typing import Any

import grpc
import pytest
from google.protobuf.empty_pb2 import Empty

from proto_fleet_sdk.generated.pb import driver_pb2 as pb
from proto_fleet_sdk.generated.pb import driver_pb2_grpc as rpc
from proto_fleet_sdk.server import PluginServer


@dataclass
class Device:
    info: pb.DeviceInfo
    recovery_state: str = "accepted recovery"


class AuthorDriver(rpc.DriverServicer):
    def __init__(self) -> None:
        self.devices: dict[str, Device] = {}
        self.creations: list[Device] = []
        self.new_calls = 0
        self.create_started = asyncio.Event()
        self.create_gate: asyncio.Event | None = None
        self.close_started = asyncio.Event()
        self.close_gate: asyncio.Event | None = None
        self.fail_close = False
        self.fail_create = False
        self.driver_description = pb.DescribeDriverResponse(driver_name="author", api_version="v1")

    async def Handshake(self, request: Empty, context: Any) -> pb.HandshakeResponse:
        return pb.HandshakeResponse(driver_name="author", api_version="v1")

    async def DescribeDriver(self, request: Empty, context: Any) -> pb.DescribeDriverResponse:
        return self.driver_description

    async def NewDevice(self, request: pb.NewDeviceRequest, context: Any) -> pb.NewDeviceResponse:
        self.new_calls += 1
        self.create_started.set()
        if self.create_gate is not None:
            await self.create_gate.wait()
        if self.fail_create:
            await context.abort(grpc.StatusCode.INTERNAL, "author creation failed")
        info = pb.DeviceInfo()
        info.CopyFrom(request.info)
        device = Device(info)
        self.devices[request.device_id] = device
        self.creations.append(device)
        return pb.NewDeviceResponse(device_id=request.device_id)

    async def CloseDevice(self, request: pb.DeviceRef, context: Any) -> Empty:
        self.close_started.set()
        if self.close_gate is not None:
            await self.close_gate.wait()
        if self.fail_close:
            await context.abort(grpc.StatusCode.INTERNAL, "author cleanup failed")
        if request.device_id not in self.devices:
            await context.abort(grpc.StatusCode.NOT_FOUND, "author has no device")
        del self.devices[request.device_id]
        return Empty()

    async def DescribeDevice(
        self, request: pb.DescribeDeviceRequest, context: Any
    ) -> pb.DescribeDeviceResponse:
        device = self.devices[request.device_id]
        return pb.DescribeDeviceResponse(
            device=device.info, caps=pb.Capabilities(flags={device.recovery_state: True})
        )

    async def Subscribe(
        self, request: pb.SubscribeRequest, context: Any
    ) -> AsyncIterator[pb.DeviceMetrics]:
        for identifier in request.device_ids:
            yield pb.DeviceMetrics(device_id=identifier)


@asynccontextmanager
async def running(driver: AuthorDriver) -> AsyncIterator[tuple[PluginServer, rpc.DriverStub]]:
    server = PluginServer(driver)
    await server.start()
    try:
        async with grpc.aio.insecure_channel(f"127.0.0.1:{server.port}") as channel:
            await channel.channel_ready()
            yield server, rpc.DriverStub(channel)
    finally:
        await server.stop(0)


def request() -> pb.NewDeviceRequest:
    return pb.NewDeviceRequest(
        device_id="miner",
        reuse_scope_key="host:identity",
        info=pb.DeviceInfo(
            host="127.0.0.1",
            port=4028,
            url_scheme="tcp",
            serial_number="serial",
            manufacturer="vendor",
            model="model",
        ),
        secret=pb.SecretBundle(
            version="v1", user_pass=pb.UsernamePassword(username="user", password="secret")
        ),
    )


async def test_scoped_reuse_preserves_actual_object_and_delegates_unary_and_stream() -> None:
    driver = AuthorDriver()
    async with running(driver) as (_, stub):
        description = await stub.DescribeDriver(Empty())
        assert description.caps.flags["sdk_scoped_device_reuse"]
        assert "sdk_scoped_device_reuse" not in driver.driver_description.caps.flags
        assert (await stub.Handshake(Empty())).driver_name == "author"
        responses = await asyncio.gather(*(stub.NewDevice(request()) for _ in range(12)))
        assert all(response.reuse_scope_key == "host:identity" for response in responses)
        assert len(driver.creations) == 1
        original = driver.devices["miner"]
        original.recovery_state = "curtailed with saved recovery profile"
        await stub.NewDevice(request())
        assert driver.devices["miner"] is original
        description = await stub.DescribeDevice(pb.DescribeDeviceRequest(device_id="miner"))
        assert description.caps.flags[original.recovery_state]
        metrics = [
            item
            async for item in stub.Subscribe(pb.SubscribeRequest(device_ids=["miner", "other"]))
        ]
        assert [item.device_id for item in metrics] == ["miner", "other"]


@pytest.mark.parametrize(
    "field",
    [
        "scope",
        "host",
        "port",
        "scheme",
        "serial",
        "manufacturer",
        "model",
        "mac",
        "firmware",
        "default_password_presence",
        "device_class",
        "username",
        "secret",
        "secret_kind",
        "version",
        "ttl",
    ],
)
async def test_any_complete_request_change_replaces_author_object(field: str) -> None:
    driver = AuthorDriver()
    first = request()
    changed = request()
    if field == "scope":
        changed.reuse_scope_key = "different host"
    elif field == "host":
        changed.info.host = "127.0.0.2"
    elif field == "port":
        changed.info.port += 1
    elif field == "scheme":
        changed.info.url_scheme = "https"
    elif field == "serial":
        changed.info.serial_number = "different"
    elif field == "manufacturer":
        changed.info.manufacturer = "other vendor"
    elif field == "model":
        changed.info.model = "other model"
    elif field == "mac":
        changed.info.mac_address = "00:11:22:33:44:55"
    elif field == "firmware":
        changed.info.firmware_version = "other firmware"
    elif field == "default_password_presence":
        changed.info.default_password_active = False
    elif field == "device_class":
        changed.info.device_class = pb.DEVICE_CLASS_CONTAINER
    elif field == "username":
        changed.secret.user_pass.username = "other user"
    elif field == "secret":
        changed.secret.user_pass.password = "changed secret"
    elif field == "secret_kind":
        changed.secret.bearer_token.token = "token"
    elif field == "version":
        changed.secret.version = "v2"
    else:
        changed.secret.ttl.seconds = 30
    async with running(driver) as (_, stub):
        await stub.NewDevice(first)
        original = driver.devices["miner"]
        await stub.NewDevice(changed)
        assert driver.devices["miner"] is not original
        replacement = driver.devices["miner"]
        await stub.NewDevice(changed)
        assert driver.devices["miner"] is replacement
        assert len(driver.creations) == 2


async def test_default_always_replaces_and_close_releases_evidence() -> None:
    driver = AuthorDriver()
    async with running(driver) as (server, stub):
        await stub.NewDevice(request())
        original = driver.devices["miner"]
        default = request()
        default.reuse_scope_key = ""
        assert not (await stub.NewDevice(default)).reuse_scope_key
        assert driver.devices["miner"] is not original
        replacement = driver.devices["miner"]
        await stub.NewDevice(default)
        assert driver.devices["miner"] is not replacement
        assert not server._scoped_servicer._reuse_keys
        await stub.NewDevice(request())
        scoped = driver.devices["miner"]
        await stub.CloseDevice(pb.DeviceRef(device_id="miner"))
        assert not server._scoped_servicer._reuse_keys
        assert not server._scoped_servicer._closing
        await stub.NewDevice(request())
        assert driver.devices["miner"] is not scoped
        assert len(server._scoped_servicer._locks) == 64


async def test_close_serializes_new_and_failure_blocks_until_cleanup_succeeds() -> None:
    driver = AuthorDriver()
    async with running(driver) as (_, stub):
        await stub.NewDevice(request())
        original = driver.devices["miner"]
        driver.close_gate = asyncio.Event()
        closing = stub.CloseDevice(pb.DeviceRef(device_id="miner"))
        await driver.close_started.wait()
        creating = stub.NewDevice(request())
        await asyncio.sleep(0.03)
        assert len(driver.creations) == 1
        driver.fail_close = True
        driver.close_gate.set()
        with pytest.raises(grpc.aio.AioRpcError) as failure:
            await closing
        assert failure.value.code() == grpc.StatusCode.INTERNAL
        with pytest.raises(grpc.aio.AioRpcError) as blocked:
            await creating
        assert blocked.value.code() == grpc.StatusCode.UNAVAILABLE
        assert driver.devices["miner"] is original
        driver.fail_close = False
        await stub.CloseDevice(pb.DeviceRef(device_id="miner"))
        await stub.NewDevice(request())
        assert driver.devices["miner"] is not original


async def test_not_found_close_allows_fresh_resolution() -> None:
    driver = AuthorDriver()
    async with running(driver) as (_, stub):
        await stub.NewDevice(request())
        driver.devices.clear()  # Author confirms that it no longer owns an object.
        with pytest.raises(grpc.aio.AioRpcError) as absent:
            await stub.CloseDevice(pb.DeviceRef(device_id="miner"))
        assert absent.value.code() == grpc.StatusCode.NOT_FOUND
        await stub.NewDevice(request())
        assert len(driver.creations) == 2


async def test_failed_create_discards_old_reuse_evidence() -> None:
    driver = AuthorDriver()
    async with running(driver) as (_, stub):
        await stub.NewDevice(request())
        driver.fail_create = True
        changed = request()
        changed.info.host = "different"
        with pytest.raises(grpc.aio.AioRpcError):
            await stub.NewDevice(changed)
        driver.fail_create = False
        await stub.NewDevice(request())
        assert len(driver.creations) == 2


async def test_canceled_async_create_does_not_publish_reuse() -> None:
    driver = AuthorDriver()
    driver.create_gate = asyncio.Event()
    async with running(driver) as (server, stub):
        canceled = stub.NewDevice(request())
        await driver.create_started.wait()
        canceled.cancel()
        with pytest.raises(asyncio.CancelledError):
            await canceled
        driver.create_gate.set()
        await stub.NewDevice(request())
        assert len(driver.creations) == 1
        assert server._scoped_servicer._reuse_keys


class SynchronousAuthorDriver(AuthorDriver):
    def __init__(self) -> None:
        super().__init__()
        self.worker_started = threading.Event()
        self.worker_gate = threading.Event()

    def NewDevice(self, request: pb.NewDeviceRequest, context: Any) -> pb.NewDeviceResponse:
        self.new_calls += 1
        self.worker_started.set()
        assert self.worker_gate.wait(5), "test did not release author worker"
        info = pb.DeviceInfo()
        info.CopyFrom(request.info)
        device = Device(info)
        self.devices[request.device_id] = device
        self.creations.append(device)
        return pb.NewDeviceResponse(device_id=request.device_id)


async def test_cancel_sync_creation_keeps_lock_until_worker_finishes() -> None:
    driver = SynchronousAuthorDriver()
    async with running(driver) as (server, stub):
        canceled = stub.NewDevice(request())
        assert await asyncio.to_thread(driver.worker_started.wait, 2)
        canceled.cancel()
        with pytest.raises(asyncio.CancelledError):
            await canceled
        replacement = stub.NewDevice(request())
        await asyncio.sleep(0.05)
        assert not replacement.done()
        assert not driver.creations
        assert driver.new_calls == 1
        driver.worker_gate.set()
        await replacement
        assert len(driver.creations) == 2
        assert driver.devices["miner"] is driver.creations[1]
        assert server._scoped_servicer._reuse_keys


class SynchronousDelegatesDriver(AuthorDriver):
    def Handshake(self, request: Empty, context: Any) -> pb.HandshakeResponse:
        return pb.HandshakeResponse(driver_name="synchronous author", api_version="v1")

    def Subscribe(self, request: pb.SubscribeRequest, context: Any) -> Any:
        for identifier in request.device_ids:
            yield pb.DeviceMetrics(device_id=identifier)


async def test_synchronous_unary_and_streaming_methods_keep_original_shape() -> None:
    async with running(SynchronousDelegatesDriver()) as (_, stub):
        assert (await stub.Handshake(Empty())).driver_name == "synchronous author"
        metrics = [
            item async for item in stub.Subscribe(pb.SubscribeRequest(device_ids=["one", "two"]))
        ]
        assert [item.device_id for item in metrics] == ["one", "two"]


async def test_default_creation_waits_for_pending_scoped_author_creation() -> None:
    driver = AuthorDriver()
    driver.create_gate = asyncio.Event()
    async with running(driver) as (_, stub):
        scoped = stub.NewDevice(request())
        await driver.create_started.wait()
        default = request()
        default.reuse_scope_key = ""
        replacement = stub.NewDevice(default)
        await asyncio.sleep(0.03)
        assert not replacement.done()
        assert driver.new_calls == 1
        driver.create_gate.set()
        await scoped
        await replacement
        assert len(driver.creations) == 2
        assert driver.devices["miner"] is driver.creations[1]
        await stub.NewDevice(request())
        assert len(driver.creations) == 3


async def test_canceled_close_remains_blocked_until_authoritative_cleanup_retry() -> None:
    driver = AuthorDriver()
    async with running(driver) as (_, stub):
        await stub.NewDevice(request())
        driver.close_gate = asyncio.Event()
        closing = stub.CloseDevice(pb.DeviceRef(device_id="miner"))
        await driver.close_started.wait()
        closing.cancel()
        with pytest.raises(asyncio.CancelledError):
            await closing
        with pytest.raises(grpc.aio.AioRpcError) as blocked:
            await stub.NewDevice(request())
        assert blocked.value.code() == grpc.StatusCode.UNAVAILABLE
        assert len(driver.creations) == 1
        driver.close_gate.set()
        await stub.CloseDevice(pb.DeviceRef(device_id="miner"))
        await stub.NewDevice(request())
        assert len(driver.creations) == 2


class SynchronousCloseDriver(AuthorDriver):
    def CloseDevice(self, request: pb.DeviceRef, context: Any) -> Empty:
        if self.fail_close:
            context.abort(grpc.StatusCode.INTERNAL, "synchronous author cleanup failed")
        if request.device_id not in self.devices:
            context.abort(grpc.StatusCode.NOT_FOUND, "synchronous author has no device")
        del self.devices[request.device_id]
        return Empty()


async def test_sync_close_abort_preserves_failure_and_cleanup_guard() -> None:
    driver = SynchronousCloseDriver()
    async with running(driver) as (_, stub):
        await stub.NewDevice(request())
        driver.fail_close = True
        with pytest.raises(grpc.aio.AioRpcError) as failure:
            await stub.CloseDevice(pb.DeviceRef(device_id="miner"))
        assert failure.value.code() == grpc.StatusCode.INTERNAL
        with pytest.raises(grpc.aio.AioRpcError) as blocked:
            await stub.NewDevice(request())
        assert blocked.value.code() == grpc.StatusCode.UNAVAILABLE
        driver.fail_close = False
        await stub.CloseDevice(pb.DeviceRef(device_id="miner"))
        with pytest.raises(grpc.aio.AioRpcError) as absent:
            await stub.CloseDevice(pb.DeviceRef(device_id="miner"))
        assert absent.value.code() == grpc.StatusCode.NOT_FOUND
        await stub.NewDevice(request())
        assert len(driver.creations) == 2
