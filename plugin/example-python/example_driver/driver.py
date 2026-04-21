"""Example Python plugin driver.

Minimal implementation of the gRPC DriverServicer interface, demonstrating
how to build a Proto Fleet plugin in Python. This serves as a starting
point for plugin authors — replace the stub logic with real device
communication.

Methods not overridden here (e.g., StartMining, StopMining, Reboot, etc.)
will return gRPC UNIMPLEMENTED automatically via the base DriverServicer.
To support a capability, override the corresponding method and declare it
in DescribeDriver's capability flags.
"""

from __future__ import annotations

import logging

import grpc
from google.protobuf.empty_pb2 import Empty
from proto_fleet_sdk.capabilities import CAP_DEVICE_STATUS, CAP_DISCOVERY, CAP_PAIRING
from proto_fleet_sdk.errors import (
    DeviceNotFoundError,
    grpc_error_handler,
)
from proto_fleet_sdk.generated.pb import driver_pb2, driver_pb2_grpc

logger = logging.getLogger(__name__)

_DRIVER_NAME = "example-python"
_API_VERSION = "v1"


class ExampleDriver(driver_pb2_grpc.DriverServicer):
    """Minimal example driver.

    Implements the required gRPC DriverServicer methods with stub responses.
    A real plugin would replace these stubs with actual device communication.
    Methods not overridden inherit the base class UNIMPLEMENTED response.
    """

    def __init__(self) -> None:
        self._devices: dict[str, driver_pb2.DeviceInfo] = {}

    # ========== Driver Info ==========

    @grpc_error_handler
    async def Handshake(
        self, request: Empty, context: grpc.ServicerContext
    ) -> driver_pb2.HandshakeResponse:
        return driver_pb2.HandshakeResponse(
            driver_name=_DRIVER_NAME, api_version=_API_VERSION
        )

    @grpc_error_handler
    async def DescribeDriver(
        self, request: Empty, context: grpc.ServicerContext
    ) -> driver_pb2.DescribeDriverResponse:
        return driver_pb2.DescribeDriverResponse(
            driver_name=_DRIVER_NAME,
            api_version=_API_VERSION,
            caps=driver_pb2.Capabilities(flags={
                CAP_DISCOVERY: True,
                CAP_PAIRING: True,
                CAP_DEVICE_STATUS: True,
            }),
        )

    @grpc_error_handler
    async def GetDiscoveryPorts(
        self, request: Empty, context: grpc.ServicerContext
    ) -> driver_pb2.GetDiscoveryPortsResponse:
        return driver_pb2.GetDiscoveryPortsResponse(ports=["80"])

    # ========== Discovery & Pairing ==========

    @grpc_error_handler
    async def DiscoverDevice(
        self, request: driver_pb2.DiscoverDeviceRequest, context: grpc.ServicerContext
    ) -> driver_pb2.DiscoverDeviceResponse:
        # A real plugin would probe the device at request.ip_address here.
        raise DeviceNotFoundError(request.ip_address)

    @grpc_error_handler
    async def PairDevice(
        self, request: driver_pb2.PairDeviceRequest, context: grpc.ServicerContext
    ) -> driver_pb2.PairDeviceResponse:
        return driver_pb2.PairDeviceResponse(device=request.device)

    @grpc_error_handler
    async def GetDefaultCredentials(
        self, request: Empty, context: grpc.ServicerContext
    ) -> driver_pb2.GetDefaultCredentialsResponse:
        return driver_pb2.GetDefaultCredentialsResponse(credentials=[])

    @grpc_error_handler
    async def GetCapabilitiesForModel(
        self, request: driver_pb2.GetCapabilitiesForModelRequest, context: grpc.ServicerContext
    ) -> driver_pb2.GetCapabilitiesForModelResponse:
        return driver_pb2.GetCapabilitiesForModelResponse(
            caps=driver_pb2.Capabilities(flags={})
        )

    # ========== Device Management ==========

    @grpc_error_handler
    async def NewDevice(
        self, request: driver_pb2.NewDeviceRequest, context: grpc.ServicerContext
    ) -> driver_pb2.NewDeviceResponse:
        self._devices[request.device_id] = request.info
        return driver_pb2.NewDeviceResponse(device_id=request.device_id)

    @grpc_error_handler
    async def DescribeDevice(
        self, request: driver_pb2.DescribeDeviceRequest, context: grpc.ServicerContext
    ) -> driver_pb2.DescribeDeviceResponse:
        device_info = self._get_device_info(request.device_id)
        return driver_pb2.DescribeDeviceResponse(
            device=device_info,
            caps=driver_pb2.Capabilities(flags={CAP_DEVICE_STATUS: True}),
        )

    @grpc_error_handler
    async def CloseDevice(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        if request.device_id not in self._devices:
            raise DeviceNotFoundError(request.device_id)
        del self._devices[request.device_id]
        return Empty()

    # ========== Telemetry ==========

    @grpc_error_handler
    async def DeviceStatus(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> driver_pb2.DeviceMetrics:
        self._get_device_info(request.device_id)
        # A real plugin would query the device for metrics here.
        return driver_pb2.DeviceMetrics(
            device_id=request.device_id,
            health=driver_pb2.HEALTH_HEALTHY_ACTIVE,
        )

    @grpc_error_handler
    async def GetErrors(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> driver_pb2.DeviceErrors:
        self._get_device_info(request.device_id)
        return driver_pb2.DeviceErrors(device_id=request.device_id, errors=[])

    @grpc_error_handler
    async def Unpair(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        self._get_device_info(request.device_id)
        return Empty()

    # ========== Helpers ==========

    def _get_device_info(self, device_id: str) -> driver_pb2.DeviceInfo:
        info = self._devices.get(device_id)
        if info is None:
            raise DeviceNotFoundError(device_id)
        return info
