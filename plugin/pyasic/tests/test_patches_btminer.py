"""Tests for WhatsMiner (BTMiner) pyasic patches."""

from __future__ import annotations

from unittest.mock import AsyncMock, MagicMock

import pytest
from proto_fleet_sdk.error_codes import MinerError

from pyasic_driver.patches import btminer


@pytest.fixture(autouse=True)
def _apply_patches():
    btminer.apply()


class TestPatchFactoryModelDetection:
    """Verify get_miner_model_whatsminer falls back to get_version."""

    @pytest.mark.asyncio
    async def test_falls_back_to_get_version(self):
        # Arrange
        from pyasic.miners.factory import MinerFactory

        factory = MinerFactory()
        factory.send_api_command = AsyncMock(side_effect=[
            None,  # devdetails fails
            {"Msg": {"miner_type": "M60S_VK40", "fw_ver": "20251209.16.Rel2"}},
        ])
        factory.send_btminer_v3_api_command = AsyncMock(return_value=None)

        # Act
        result = await factory.get_miner_model_whatsminer("172.16.2.58")

        # Assert
        assert result == "M60SVK40"

    @pytest.mark.asyncio
    async def test_returns_none_when_all_fail(self):
        # Arrange
        from pyasic.miners.factory import MinerFactory

        factory = MinerFactory()
        factory.send_api_command = AsyncMock(return_value=None)
        factory.send_btminer_v3_api_command = AsyncMock(return_value=None)

        # Act
        result = await factory.get_miner_model_whatsminer("172.16.2.58")

        # Assert
        assert result is None


class TestPatchForceV2:
    """Verify BTMiner.__new__ always selects V2 backend."""

    def test_v3_firmware_gets_v2_backend(self):
        # Arrange
        from pyasic.miners.backends.btminer import BTMiner
        from pyasic.rpc.btminer import BTMinerRPCAPI

        # Act — firmware date > 2024.11.0 would normally select V3
        miner = BTMiner("10.255.255.1", "20251209")

        # Assert
        assert isinstance(miner.rpc, BTMinerRPCAPI)


class TestPatchMulticommand:
    """Verify multicommand normalizes summary responses."""

    @pytest.mark.asyncio
    async def test_normalizes_msg_format_to_summary(self):
        # Arrange
        from pyasic.rpc.btminer import BTMinerRPCAPI

        rpc = BTMinerRPCAPI("10.255.255.4")
        rpc._check_commands = MagicMock(side_effect=lambda *cmds: list(cmds))
        rpc._send_split_multicommand = AsyncMock(return_value={
            "summary": [{
                "STATUS": "S",
                "Msg": {"Elapsed": 100, "MHS 1m": 50.0, "Power": 3000},
            }],
        })

        # Act
        result = await rpc.multicommand("summary")

        # Assert
        normalized = result["summary"][0]
        assert "SUMMARY" in normalized
        assert normalized["SUMMARY"][0]["Power"] == 3000

    @pytest.mark.asyncio
    async def test_preserves_cgminer_format(self):
        # Arrange
        from pyasic.rpc.btminer import BTMinerRPCAPI

        rpc = BTMinerRPCAPI("10.255.255.5")
        cgminer_response = {
            "STATUS": [{"STATUS": "S", "Msg": "Summary"}],
            "SUMMARY": [{"Elapsed": 100, "MHS av": 50.0}],
            "id": 1,
        }
        rpc._check_commands = MagicMock(side_effect=lambda *cmds: list(cmds))
        rpc._send_split_multicommand = AsyncMock(return_value={
            "summary": [cgminer_response],
        })

        # Act
        result = await rpc.multicommand("summary")

        # Assert
        assert result["summary"][0] == cgminer_response


class TestPatchHashboards:
    """Verify hashboard parsing handles missing fields and unit detection."""

    def _make_miner(self):
        from pyasic.device.algorithm.hashrate.sha256 import SHA256HashRate, SHA256Unit

        miner = MagicMock()
        miner.expected_hashboards = 3
        miner.expected_chips = 100
        miner.algo.unit = SHA256Unit
        miner.algo.hashrate = SHA256HashRate
        return miner

    @pytest.mark.asyncio
    async def test_missing_chip_temp_avg(self):
        """Board without Chip Temp Avg should still populate other fields."""
        # Arrange
        from pyasic.miners.backends.btminer import BTMinerV2

        miner = self._make_miner()
        rpc_devs = {"DEVS": [{
            "ASC": 0,
            "Temperature": 42.5,
            "MHS 1m": 58.97,
            "Factory GHS": 200,
            "Effective Chips": 100,
            "PCB SN": "ABC123",
        }]}

        # Act
        boards = await BTMinerV2._get_hashboards(miner, rpc_devs=rpc_devs)

        # Assert
        assert boards[0].temp == 42  # round(42.5) = 42 (banker's rounding)
        assert boards[0].chip_temp is None
        assert boards[0].chips == 100
        assert boards[0].missing is False

    @pytest.mark.asyncio
    async def test_detects_ths_mislabeled_as_mhs(self):
        """When MHS 1m < Factory GHS, value is actually in TH/s."""
        # Arrange
        from pyasic.miners.backends.btminer import BTMinerV2

        miner = self._make_miner()
        rpc_devs = {"DEVS": [{
            "ASC": 0,
            "Temperature": 40,
            "Chip Temp Avg": 45,
            "MHS 1m": 58.97,       # Actually TH/s (mislabeled)
            "Factory GHS": 200,     # 200 GH/s → 58.97 < 200 → TH/s
            "Effective Chips": 100,
            "PCB SN": "ABC123",
        }]}

        # Act
        boards = await BTMinerV2._get_hashboards(miner, rpc_devs=rpc_devs)

        # Assert — 58.97 TH/s should stay ~58.97 TH/s (not be divided by 1e6)
        assert boards[0].hashrate.rate == pytest.approx(58.97, abs=0.01)

    @pytest.mark.asyncio
    async def test_real_mhs_stays_mhs(self):
        """When MHS 1m > Factory GHS, value is in real MH/s."""
        # Arrange
        from pyasic.miners.backends.btminer import BTMinerV2

        miner = self._make_miner()
        rpc_devs = {"DEVS": [{
            "ASC": 0,
            "Temperature": 40,
            "MHS 1m": 171000000.0,  # Real MH/s
            "Factory GHS": 200,
            "Effective Chips": 100,
            "PCB SN": "ABC123",
        }]}

        # Act
        boards = await BTMinerV2._get_hashboards(miner, rpc_devs=rpc_devs)

        # Assert — 171M MH/s = 171 TH/s
        assert boards[0].hashrate.rate == pytest.approx(171.0, abs=0.01)


class TestPatchPrivilegedCommands:
    """Verify privileged commands treat empty/error responses as success."""

    @pytest.mark.asyncio
    async def test_empty_response_is_success(self):
        # Arrange
        from pyasic.miners.backends.btminer import BTMinerV2

        miner = MagicMock()
        miner.rpc.reboot = AsyncMock(return_value={})

        # Act
        result = await BTMinerV2.reboot(miner)

        # Assert
        assert result is True

    @pytest.mark.asyncio
    async def test_api_error_is_success(self):
        # Arrange
        from pyasic.errors import APIError
        from pyasic.miners.backends.btminer import BTMinerV2

        miner = MagicMock()
        miner.rpc.power_off = AsyncMock(side_effect=APIError("timeout"))

        # Act
        result = await BTMinerV2.stop_mining(miner)

        # Assert
        assert result is True


class TestPatchSendConfig:
    """Verify send_config propagates pool update errors."""

    @pytest.mark.asyncio
    async def test_pool_update_error_propagates(self):
        # Arrange
        from pyasic.errors import APIError
        from pyasic.miners.backends.btminer import BTMinerV2

        miner = MagicMock()
        miner.rpc.update_pools = AsyncMock(side_effect=APIError("auth failed"))
        config = MagicMock()
        config.as_wm.return_value = {
            "pools": {"pool1": "stratum+tcp://pool:3333"},
            "mode": "normal",
        }

        # Act / Assert
        with pytest.raises(APIError):
            await BTMinerV2.send_config(miner, config)

    @pytest.mark.asyncio
    async def test_power_mode_error_is_caught(self):
        # Arrange
        from pyasic.errors import APIError
        from pyasic.miners.backends.btminer import BTMinerV2

        miner = MagicMock()
        miner.rpc.update_pools = AsyncMock(return_value={"Msg": "OK"})
        miner.rpc.set_normal_power = AsyncMock(side_effect=APIError("unsupported"))
        config = MagicMock()
        config.as_wm.return_value = {
            "pools": {"pool1": "stratum+tcp://pool:3333"},
            "mode": "normal",
        }

        # Act — should not raise
        await BTMinerV2.send_config(miner, config)

        # Assert — power mode was attempted
        miner.rpc.set_normal_power.assert_called_once()


_has_infer_miner_error = hasattr(
    __import__("pyasic_driver.device", fromlist=["_infer_miner_error"]),
    "_infer_miner_error",
)


@pytest.mark.skipif(not _has_infer_miner_error, reason="requires _infer_miner_error from plugin hardening PR")
class TestPatchErrorCodes:
    """Verify WhatsMiner numeric error codes are mapped by _infer_miner_error."""

    def test_whatsminer_fan_code(self):
        # Arrange
        from pyasic_driver.device import _infer_miner_error

        # Act
        result = _infer_miner_error("some error", error_code=100)

        # Assert
        assert result == MinerError.FAN_FAILED

    def test_whatsminer_psu_code(self):
        # Arrange
        from pyasic_driver.device import _infer_miner_error

        # Act
        result = _infer_miner_error("some error", error_code=205)

        # Assert
        assert result == MinerError.PSU_OUTPUT_OVERCURRENT

    def test_whatsminer_hashboard_code(self):
        # Arrange
        from pyasic_driver.device import _infer_miner_error

        # Act
        result = _infer_miner_error("some error", error_code=500)

        # Assert
        assert result == MinerError.HASHBOARD_NOT_PRESENT

    def test_unknown_code_falls_through_to_keyword(self):
        # Arrange
        from pyasic_driver.device import _infer_miner_error

        # Act
        result = _infer_miner_error("fan speed deviation", error_code=99999)

        # Assert
        assert result == MinerError.FAN_FAILED

    def test_unknown_code_and_message_returns_unmapped(self):
        # Arrange
        from pyasic_driver.device import _infer_miner_error

        # Act
        result = _infer_miner_error("something unknown", error_code=99999)

        # Assert
        assert result == MinerError.VENDOR_ERROR_UNMAPPED


class TestPatchAuthError:
    """Verify BTMinerV3AuthError is converted to AuthenticationFailedError."""

    def test_patch_wraps_get_data(self):
        """The patch replaces BTMinerV2.get_data with a wrapper."""
        # Arrange
        from pyasic.miners.backends.btminer import BTMinerV2

        # Assert
        assert BTMinerV2.get_data.__name__ == "get_data_with_auth_check"
