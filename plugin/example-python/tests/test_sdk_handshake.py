"""Launch the actual example entry point through the SDK go-plugin handshake."""

import asyncio
import os
import sys
from pathlib import Path

import grpc
from google.protobuf.empty_pb2 import Empty
from proto_fleet_sdk.generated.pb import driver_pb2 as pb
from proto_fleet_sdk.generated.pb import driver_pb2_grpc as rpc


async def test_example_process_handshake_and_scoped_resolution() -> None:
    plugin_root = Path(__file__).resolve().parents[1]
    env = dict(os.environ, MINER_DRIVER_PLUGIN="fleet-miner-driver")
    process = await asyncio.create_subprocess_exec(
        sys.executable,
        str(plugin_root / "main.py"),
        cwd=plugin_root,
        env=env,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.DEVNULL,
    )
    try:
        assert process.stdout is not None
        advertisement = (await asyncio.wait_for(process.stdout.readline(), 5)).decode().strip()
        core, app, network, address, protocol = advertisement.split("|")
        assert (core, app, network, protocol) == ("1", "1", "tcp", "grpc")
        async with grpc.aio.insecure_channel(address) as channel:
            await asyncio.wait_for(channel.channel_ready(), 5)
            driver = rpc.DriverStub(channel)
            assert (await driver.Handshake(Empty())).driver_name == "example-python"
            description = await driver.DescribeDriver(Empty())
            assert description.caps.flags["sdk_scoped_device_reuse"]
            request = pb.NewDeviceRequest(
                device_id="example",
                reuse_scope_key="host:miner",
                info=pb.DeviceInfo(host="127.0.0.1", port=80),
            )
            for _ in range(2):
                response = await driver.NewDevice(request)
                assert response.reuse_scope_key == request.reuse_scope_key
            request.info.host = "127.0.0.2"
            response = await driver.NewDevice(request)
            resolved = await driver.DescribeDevice(
                pb.DescribeDeviceRequest(device_id="example", backend_generation=response.backend_generation)
            )
            assert resolved.device.host == "127.0.0.2"
            await driver.CloseDevice(
                pb.DeviceRef(device_id="example", backend_generation=response.backend_generation)
            )
            response = await driver.NewDevice(request)
            assert (
                await driver.DescribeDevice(
                    pb.DescribeDeviceRequest(
                        device_id="example", backend_generation=response.backend_generation
                    )
                )
            ).device.host == "127.0.0.2"
    finally:
        # This test owns the fake plugin process; cleanup does not depend on
        # the entry point's unrelated graceful signal handling.
        if process.returncode is None:
            process.kill()
        await process.wait()
