"""Offline API smoke tests for a running Mini Eureka Compose cluster.

Run with: pytest -q tests/api_smoke.py
"""

from __future__ import annotations

import json
import os
import time
import uuid
from dataclasses import dataclass
from typing import Any
from urllib.error import HTTPError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen

import pytest


BASE_URL = os.getenv("MINIEUREKA_BASE_URL", "http://127.0.0.1:18780").rstrip("/")
NODE_URLS = [
    value.rstrip("/")
    for value in os.getenv(
        "MINIEUREKA_NODE_URLS",
        "http://127.0.0.1:18781,http://127.0.0.1:18782,http://127.0.0.1:18783",
    ).split(",")
    if value
]


@dataclass(frozen=True)
class Response:
    status: int
    headers: Any
    body: Any


def request(
    method: str,
    path: str,
    payload: Any | None = None,
    *,
    base_url: str = BASE_URL,
    expected: tuple[int, ...] = (200,),
) -> Response:
    data = None
    headers = {"Accept": "application/json"}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = Request(f"{base_url}{path}", data=data, headers=headers, method=method)
    try:
        with urlopen(req, timeout=5) as response:
            raw = response.read()
            body = json.loads(raw) if raw else None
            result = Response(response.status, response.headers, body)
    except HTTPError as error:
        raw = error.read()
        body = json.loads(raw) if raw else None
        result = Response(error.code, error.headers, body)
    assert result.status in expected, result.body
    return result


def wait_until(predicate, timeout: float = 8.0, interval: float = 0.2) -> None:
    deadline = time.monotonic() + timeout
    last_error: BaseException | None = None
    while time.monotonic() < deadline:
        try:
            if predicate():
                return
        except (AssertionError, OSError, HTTPError) as error:
            last_error = error
        time.sleep(interval)
    if last_error:
        raise AssertionError(f"condition not met before timeout: {last_error}") from last_error
    raise AssertionError("condition not met before timeout")


def items(response: Response) -> list[dict[str, Any]]:
    data = response.body.get("data", [])
    assert isinstance(data, list), response.body
    return data


@pytest.fixture(scope="module")
def registered_instance() -> dict[str, str]:
    suffix = uuid.uuid4().hex[:10]
    service = f"smoke-{suffix}"
    instance = f"instance-{suffix}"
    registration_id = f"registration-{uuid.uuid4().hex}"
    path = f"/api/v1/services/{quote(service)}/instances"
    response = request(
        "POST",
        path,
        {
            "instance_id": instance,
            "registration_id": registration_id,
            "host": "127.0.0.1",
            "port": 19090,
            "protocol": "http",
            "metadata": {"suite": "api-smoke"},
        },
        expected=(200, 201),
    )
    record = response.body["data"]
    assert record["service"] == service
    assert record["instance_id"] == instance
    assert record["status"] == "ACTIVE"
    assert record["lease_id"]

    duplicate = request("POST", path, {
        "instance_id": instance,
        "registration_id": registration_id,
        "host": "127.0.0.1",
        "port": 19090,
        "protocol": "http",
        "metadata": {"suite": "api-smoke"},
    }, expected=(200, 201))
    assert duplicate.body["data"]["lease_id"] == record["lease_id"]

    yield {
        "service": service,
        "instance": instance,
        "lease_id": record["lease_id"],
    }

    query = urlencode({"lease_id": record["lease_id"], "operation_id": f"cleanup-{suffix}"})
    request(
        "DELETE",
        f"/api/v1/services/{quote(service)}/instances/{quote(instance)}?{query}",
        expected=(204, 404, 409),
    )


def test_health_and_readiness() -> None:
    for path in ("/healthz", "/readyz"):
        response = request("GET", path)
        assert response.body["status"] in {"ok", "ready"}


def test_rejects_invalid_registration() -> None:
    suffix = uuid.uuid4().hex[:8]
    response = request(
        "POST",
        f"/api/v1/services/invalid-{suffix}/instances",
        {
            "instance_id": "broken",
            "registration_id": f"reg-{suffix}",
            "host": "127.0.0.1",
            "port": 70000,
            "protocol": "http",
        },
        expected=(400, 422),
    )
    assert response.body["error"]["code"] in {"validation_error", "invalid_request"}


def test_heartbeat_and_discovery(registered_instance: dict[str, str]) -> None:
    service = registered_instance["service"]
    instance = registered_instance["instance"]
    heartbeat = request(
        "PUT",
        f"/api/v1/services/{quote(service)}/instances/{quote(instance)}/heartbeat",
        {
            "lease_id": registered_instance["lease_id"],
            "operation_id": f"heartbeat-{uuid.uuid4().hex}",
        },
    )
    assert heartbeat.body["data"]["status"] == "ACTIVE"

    discovery_path = f"/api/v1/services/{quote(service)}/instances"
    discovered = items(request("GET", discovery_path))
    assert any(row["instance_id"] == instance for row in discovered)

    for node_url in NODE_URLS:
        def converged(url: str = node_url) -> bool:
            rows = items(request("GET", discovery_path, base_url=url))
            return any(row["instance_id"] == instance for row in rows)

        wait_until(converged)


def test_stale_lease_is_rejected(registered_instance: dict[str, str]) -> None:
    response = request(
        "PUT",
        (
            f"/api/v1/services/{quote(registered_instance['service'])}/instances/"
            f"{quote(registered_instance['instance'])}/heartbeat"
        ),
        {
            "lease_id": "lease-intentionally-stale",
            "operation_id": f"stale-{uuid.uuid4().hex}",
        },
        expected=(409,),
    )
    assert response.body["error"]["code"] == "stale_lease"


def test_dashboard_and_topology_contract(registered_instance: dict[str, str]) -> None:
    snapshot = request("GET", "/api/v1/dashboard/snapshot?limit=10000")
    assert isinstance(snapshot.body["data"]["instances"], list)
    assert isinstance(snapshot.body["data"]["nodes"], list)
    assert isinstance(snapshot.body["data"]["edges"], list)
    assert isinstance(snapshot.body["data"]["recent_events"], list)
    assert isinstance(snapshot.body["meta"]["event_cursor"], int)
    assert "demo_enabled" in snapshot.body["data"]["capabilities"]

    topology = request("GET", "/api/v1/cluster/topology")
    assert isinstance(topology.body["data"]["nodes"], list)
    assert isinstance(topology.body["data"]["edges"], list)

    service = registered_instance["service"]
    instance = registered_instance["instance"]
    assert any(
        row["service"] == service and row["instance_id"] == instance
        for row in snapshot.body["data"]["instances"]
    )


def test_metrics_are_prometheus_text() -> None:
    req = Request(f"{BASE_URL}/metrics", headers={"Accept": "text/plain"})
    with urlopen(req, timeout=5) as response:
        body = response.read().decode("utf-8")
    assert response.status == 200
    assert "minieureka_" in body


def test_demo_offline_reaches_all_nodes_and_emits_real_delivery() -> None:
    snapshot = request("GET", "/api/v1/dashboard/snapshot?limit=10000")
    candidates = [
        row
        for row in snapshot.body["data"]["instances"]
        if row.get("demo") and row["status"] != "EVICTED"
    ]
    if not candidates:
        pytest.skip("the demo dataset has no remaining online instance")
    target = candidates[0]
    operation_id = f"offline-smoke-{uuid.uuid4().hex}"
    response = request(
        "POST",
        (
            f"/api/v1/demo/services/{quote(target['service'])}/instances/"
            f"{quote(target['instance_id'])}/offline"
        ),
        {"lease_id": target["lease_id"], "operation_id": operation_id},
        expected=(202,),
    )
    event_id = response.body["data"]["event_id"]
    assert event_id
    assert response.body["data"]["status"] == "EVICTED"

    for node_url in NODE_URLS:
        def evicted_everywhere(url: str = node_url) -> bool:
            state = request(
                "GET",
                (
                    "/api/v1/dashboard/snapshot?limit=10000&"
                    + urlencode({"service": target["service"]})
                ),
                base_url=url,
            )
            return any(
                row["instance_id"] == target["instance_id"]
                and row["status"] == "EVICTED"
                for row in state.body["data"]["instances"]
            )

        wait_until(evicted_everywhere, timeout=12.0)

    def has_delivery_receipt() -> bool:
        for node_url in NODE_URLS:
            event_response = request(
                "GET",
                f"/api/v1/gossip/events?limit=200&event_id={quote(event_id)}",
                base_url=node_url,
            )
            if any(event.get("delivery") for event in event_response.body["data"]):
                return True
        return False

    wait_until(has_delivery_receipt, timeout=12.0)
