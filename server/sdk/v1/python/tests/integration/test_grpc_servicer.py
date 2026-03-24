"""Integration tests for gRPC servicer."""

import pytest
import grpc.aio

from proto_fleet_sdk.generated.pb import driver_pb2, driver_pb2_grpc


@pytest.fixture
async def driver_stub():
    """Start a plugin server and yield a connected DriverStub."""
    from proto_fleet_sdk.server import PluginServer
    from tests.conftest import StubDriver

    driver = StubDriver()
    server = PluginServer(driver, port=0, host="localhost")
    await server.start()

    try:
        async with grpc.aio.insecure_channel(f"localhost:{server.port}") as channel:
            yield driver_pb2_grpc.DriverStub(channel)
    finally:
        await server.stop()


@pytest.mark.asyncio
async def test_handshake_roundtrip(driver_stub):
    """Test handshake RPC end-to-end."""
    from google.protobuf.empty_pb2 import Empty

    response = await driver_stub.Handshake(Empty())

    assert response.driver_name == "stub-plugin"
    assert response.api_version == "v1"


@pytest.mark.asyncio
async def test_get_discovery_ports_roundtrip(driver_stub):
    """Test optional discovery-port RPC end-to-end."""
    from google.protobuf.empty_pb2 import Empty

    response = await driver_stub.GetDiscoveryPorts(Empty())

    assert response.ports == ["443", "8080"]


@pytest.mark.asyncio
async def test_device_lifecycle(driver_stub):
    """Test device creation, status, and cleanup."""
    # Create device
    device_info = driver_pb2.DeviceInfo(
        host="192.168.1.100",
        port=80,
        url_scheme="http",
        serial_number="TEST123",
        model="TestMiner",
        manufacturer="TestCo",
    )

    secret = driver_pb2.SecretBundle(
        version="1",
        user_pass=driver_pb2.UsernamePassword(
            username="root",
            password="root"
        )
    )

    new_device_response = await driver_stub.NewDevice(
        driver_pb2.NewDeviceRequest(
            device_id="test-device-1",
            info=device_info,
            secret=secret,
        )
    )

    assert new_device_response.device_id == "test-device-1"

    # Get status and verify telemetry values
    status = await driver_stub.DeviceStatus(
        driver_pb2.DeviceRef(device_id="test-device-1")
    )

    assert status.device_id == "test-device-1"
    assert status.health in [
        driver_pb2.HEALTH_HEALTHY_ACTIVE,
        driver_pb2.HEALTH_HEALTHY_INACTIVE,
    ]
    assert status.hashrate_hs.value == pytest.approx(110.0e12)
    assert status.temp_c.value == pytest.approx(65.0)
    assert status.power_w.value == pytest.approx(3250.0)
    assert status.fan_rpm.value == pytest.approx(4500.0)
    assert len(status.hash_boards) == 3
    assert len(status.fan_metrics) == 2

    # Close device
    await driver_stub.CloseDevice(
        driver_pb2.DeviceRef(device_id="test-device-1")
    )


@pytest.mark.asyncio
async def test_error_mapping(driver_stub):
    """Test SDK exception mapping to gRPC status codes."""
    # Try to get status of non-existent device
    with pytest.raises(grpc.aio.AioRpcError) as exc_info:
        await driver_stub.DeviceStatus(
            driver_pb2.DeviceRef(device_id="non-existent")
        )

    # Should map to NOT_FOUND status code
    assert exc_info.value.code() == grpc.StatusCode.NOT_FOUND
