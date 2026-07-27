import base64
import importlib.util
import json
from pathlib import Path
import unittest
from unittest import mock


MODULE_PATH = Path(__file__).parents[1] / "scripts" / "observe_generation.py"
SPEC = importlib.util.spec_from_file_location("observe_generation", MODULE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"unable to load observer from {MODULE_PATH}")
observe_generation = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(observe_generation)


def encoded(value: str) -> str:
    return base64.b64encode(value.encode()).decode()


def dcs_snapshot(
    *,
    leader: str = "patroni-a",
    create_revision: str = "41",
    lease: str = "998",
    mod_revision: str = "47",
    revision: str = "50",
    include_member: bool = True,
) -> dict:
    cluster_path = "/service/proto-fleet-ha07"
    key_values = [
        {
            "key": encoded(f"{cluster_path}/leader"),
            "value": encoded(leader),
            "create_revision": create_revision,
            "mod_revision": mod_revision,
            "lease": lease,
        }
    ]
    if include_member:
        key_values.append(
            {
                "key": encoded(f"{cluster_path}/members/{leader}"),
                "value": encoded(
                    json.dumps(
                        {
                            "api_url": f"http://{leader}:8008/patroni",
                            "conn_url": f"postgres://{leader}:5432/postgres",
                            "role": "primary",
                            "state": "running",
                        }
                    )
                ),
                "create_revision": "13",
                "mod_revision": "48",
            }
        )
    return {"header": {"revision": revision}, "kvs": key_values}


class ExtractDcsObservationTest(unittest.TestCase):
    def test_extracts_stable_generation_and_leader_binding(self) -> None:
        observation = observe_generation.extract_dcs_observation(
            dcs_snapshot(), "/service/proto-fleet-ha07"
        )

        self.assertEqual(observation["writer_generation"], 41)
        self.assertEqual(observation["leader_name"], "patroni-a")
        self.assertEqual(
            observation["member"]["conn_url"],
            "postgres://patroni-a:5432/postgres",
        )
        self.assertEqual(observation["dcs_revision"], 50)
        self.assertEqual(observation["leader_mod_revision"], 47)
        self.assertEqual(observation["leader_lease_id"], 998)

    def test_rejects_snapshot_without_leader(self) -> None:
        with self.assertRaisesRegex(ValueError, "leader key"):
            observe_generation.extract_dcs_observation(
                {"header": {"revision": "50"}, "kvs": []},
                "/service/proto-fleet-ha07",
            )

    def test_rejects_snapshot_without_matching_member(self) -> None:
        with self.assertRaisesRegex(ValueError, "member key"):
            observe_generation.extract_dcs_observation(
                dcs_snapshot(include_member=False), "/service/proto-fleet-ha07"
            )

    def test_rejects_non_positive_generation(self) -> None:
        with self.assertRaisesRegex(ValueError, "create_revision"):
            observe_generation.extract_dcs_observation(
                dcs_snapshot(create_revision="0"), "/service/proto-fleet-ha07"
            )


class PrefixRangeEndTest(unittest.TestCase):
    def test_advances_the_last_prefix_byte(self) -> None:
        self.assertEqual(
            observe_generation.prefix_range_end(b"/service/proto-fleet-ha07/"),
            b"/service/proto-fleet-ha070",
        )


class WriterAddressBindingTest(unittest.TestCase):
    def test_accepts_the_member_connection_address(self) -> None:
        def resolve(_host: str, _port: int, **_kwargs: object) -> list:
            return [(None, None, None, None, ("172.30.0.12", 5432))]

        self.assertTrue(
            observe_generation.writer_address_matches(
                "172.30.0.12",
                5432,
                "postgres://patroni-a:5432/postgres",
                resolver=resolve,
            )
        )

    def test_rejects_a_different_writable_server(self) -> None:
        def resolve(_host: str, _port: int, **_kwargs: object) -> list:
            return [(None, None, None, None, ("172.30.0.13", 5432))]

        self.assertFalse(
            observe_generation.writer_address_matches(
                "172.30.0.12",
                5432,
                "postgres://patroni-a:5432/postgres",
                resolver=resolve,
            )
        )


class ObserveTest(unittest.TestCase):
    def observe_with(
        self,
        snapshots: list[dict],
        *,
        address_matches: bool = True,
        writer_timeline: int = 7,
        patroni_timeline: int = 7,
    ) -> dict:
        with (
            mock.patch.object(
                observe_generation,
                "read_linearizable_snapshot",
                side_effect=snapshots,
            ),
            mock.patch.object(
                observe_generation,
                "query_writable_postgres",
                return_value={
                    "server_address": "172.30.0.12",
                    "server_port": 5432,
                    "timeline": writer_timeline,
                },
            ),
            mock.patch.object(
                observe_generation,
                "query_patroni",
                return_value={"role": "primary", "timeline": patroni_timeline},
            ),
            mock.patch.object(
                observe_generation,
                "writer_address_matches",
                return_value=address_matches,
            ),
            mock.patch.object(
                observe_generation,
                "read_lease_ttl",
                return_value={"granted_ttl": 20, "ttl": 18},
            ),
        ):
            return observe_generation.observe(
                dcs_endpoint="http://etcd-a:2379",
                db_dsn="postgresql://patroni-a,patroni-b/postgres",
                cluster_path="/service/proto-fleet-ha07",
                timeout_seconds=1,
            )

    def test_rejects_writable_server_address_mismatch(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "does not match"):
            self.observe_with([dcs_snapshot()], address_matches=False)

    def test_rejects_timeline_mismatch(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "timeline does not match"):
            self.observe_with(
                [dcs_snapshot()],
                writer_timeline=7,
                patroni_timeline=8,
            )

    def test_rejects_leadership_change_during_validation(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "leader term changed"):
            self.observe_with(
                [
                    dcs_snapshot(),
                    dcs_snapshot(
                        leader="patroni-b",
                        create_revision="51",
                        mod_revision="53",
                        revision="54",
                    ),
                ]
            )

    def test_reports_post_validation_revision_for_stable_term(self) -> None:
        observation = self.observe_with(
            [
                dcs_snapshot(),
                dcs_snapshot(mod_revision="58", revision="61"),
            ]
        )

        self.assertEqual(observation["dcs"]["revision"], 61)
        self.assertEqual(observation["dcs"]["leader_mod_revision"], 58)
        self.assertEqual(observation["dcs"]["leader_lease_id"], 998)
        self.assertEqual(observation["dcs"]["leader_lease_ttl"], 18)
        self.assertEqual(observation["writer_generation"], 41)


if __name__ == "__main__":
    unittest.main()
