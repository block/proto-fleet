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
    assert received.password == password


@pytest.mark.parametrize("password", [None, "", "stored-password"])
def test_pool_readback_distinguishes_unavailable_and_empty(password: str | None) -> None:
    sent = driver_pb2.ConfiguredPool(
        priority=0, url="stratum+tcp://pool.example:3333", username="wallet.worker"
    )
    if password is not None:
        sent.password = password
    received = driver_pb2.ConfiguredPool.FromString(sent.SerializeToString())
    assert received.HasField("password") is (password is not None)
    if password is not None:
        assert received.password == password
