"""Pool credentials retain wire values and readback availability."""

import pytest

from proto_fleet_sdk.generated.pb import driver_pb2


@pytest.mark.parametrize("password", ["", "pool-password"])
def test_pool_write_password_survives_wire_round_trip(password: str) -> None:
    sent = driver_pb2.MiningPool(
        priority=0,
        url="stratum+tcp://pool.example:3333",
        worker_name="wallet.worker",
        password=password,
    )
    received = driver_pb2.MiningPool.FromString(sent.SerializeToString())
    assert received.HasField("password")
    assert received.password == password


def test_pool_write_without_password_retains_absence() -> None:
    sent = driver_pb2.MiningPool(
        priority=0,
        url="stratum+tcp://pool.example:3333",
        worker_name="wallet.worker",
    )
    received = driver_pb2.MiningPool.FromString(sent.SerializeToString())
    assert not received.HasField("password")


def test_runtime_pool_readback_has_no_password_field() -> None:
    assert "password" not in driver_pb2.ConfiguredPool.DESCRIPTOR.fields_by_name
