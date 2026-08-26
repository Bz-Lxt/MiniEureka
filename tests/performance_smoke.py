"""Configurable local load probe for Mini Eureka.

The default is intentionally small. The requirements baseline is run with:
  python3 tests/performance_smoke.py --services 200 --instances 20000 \
    --duration 60 --workers 128 --assert-baseline
"""

from __future__ import annotations

import argparse
import concurrent.futures
import json
import math
import random
import statistics
import threading
import time
import uuid
from dataclasses import dataclass, field
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen


@dataclass
class Lease:
    service: str
    instance: str
    lease_id: str
    operation: int = 0
    lock: threading.Lock = field(default_factory=threading.Lock, repr=False)

    def next_operation(self) -> str:
        with self.lock:
            self.operation += 1
            return f"perf-heartbeat-{self.instance}-{self.operation}"


@dataclass
class Sample:
    kind: str
    elapsed_ms: float
    ok: bool
    status: int


def percentile(values: list[float], quantile: float) -> float:
    if not values:
        return math.nan
    ordered = sorted(values)
    index = min(len(ordered) - 1, math.ceil(quantile * len(ordered)) - 1)
    return ordered[index]


class Client:
    def __init__(self, base_url: str) -> None:
        self.base_url = base_url.rstrip("/")

    def call(self, method: str, path: str, payload: Any | None = None) -> tuple[int, Any, float]:
        data = None
        headers = {"Accept": "application/json", "Connection": "keep-alive"}
        if payload is not None:
            data = json.dumps(payload, separators=(",", ":")).encode("utf-8")
            headers["Content-Type"] = "application/json"
        request = Request(f"{self.base_url}{path}", data=data, headers=headers, method=method)
        started = time.perf_counter()
        try:
            with urlopen(request, timeout=5) as response:
                raw = response.read()
                body = json.loads(raw) if raw else None
                status = response.status
        except HTTPError as error:
            raw = error.read()
            body = json.loads(raw) if raw else None
            status = error.code
        elapsed = (time.perf_counter() - started) * 1000
        return status, body, elapsed

    def register(self, service: str, instance: str, registration_id: str) -> tuple[Lease | None, Sample]:
        status, body, elapsed = self.call(
            "POST",
            f"/api/v1/services/{quote(service)}/instances",
            {
                "instance_id": instance,
                "registration_id": registration_id,
                "host": "127.0.0.1",
                "port": 10000 + (hash(instance) % 50000),
                "protocol": "http",
                "metadata": {"source": "performance-smoke"},
            },
        )
        ok = status in (200, 201) and isinstance(body, dict) and body.get("data", {}).get("lease_id")
        lease = Lease(service, instance, body["data"]["lease_id"]) if ok else None
        return lease, Sample("register", elapsed, bool(ok), status)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default="http://127.0.0.1:18781")
    parser.add_argument("--services", type=int, default=20)
    parser.add_argument("--instances", type=int, default=1000)
    parser.add_argument("--duration", type=float, default=10)
    parser.add_argument("--workers", type=int, default=64)
    parser.add_argument("--assert-baseline", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.services < 1 or args.instances < 1 or args.workers < 1 or args.duration <= 0:
        raise SystemExit("services, instances, workers and duration must be positive")

    client = Client(args.base_url)
    run_id = uuid.uuid4().hex[:8]
    leases: list[Lease] = []
    samples: list[Sample] = []
    samples_lock = threading.Lock()

    def create(index: int) -> None:
        service = f"perf-{run_id}-{index % args.services:04d}"
        instance = f"instance-{index:06d}"
        lease, sample = client.register(service, instance, f"registration-{run_id}-{index}")
        with samples_lock:
            samples.append(sample)
            if lease:
                leases.append(lease)

    started = time.perf_counter()
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as executor:
        list(executor.map(create, range(args.instances)))
    registration_seconds = time.perf_counter() - started
    preload_sample_count = len(samples)
    if not leases:
        print(json.dumps({"error": "all registrations failed"}, ensure_ascii=False))
        return 1

    stop_at = time.monotonic() + args.duration
    counter = 0
    counter_lock = threading.Lock()

    def mixed_worker(worker_id: int) -> None:
        nonlocal counter
        rng = random.Random((worker_id + 1) * 7919)
        while time.monotonic() < stop_at:
            roll = rng.random()
            if roll < 0.70:
                lease = leases[rng.randrange(len(leases))]
                status, _, elapsed = client.call(
                    "PUT",
                    f"/api/v1/services/{quote(lease.service)}/instances/{quote(lease.instance)}/heartbeat",
                    {"lease_id": lease.lease_id, "operation_id": lease.next_operation()},
                )
                sample = Sample("heartbeat", elapsed, status == 200, status)
            elif roll < 0.90:
                service = leases[rng.randrange(len(leases))].service
                status, _, elapsed = client.call(
                    "GET", f"/api/v1/services/{quote(service)}/instances?limit=500"
                )
                sample = Sample("discover", elapsed, status == 200, status)
            else:
                with counter_lock:
                    counter += 1
                    ephemeral_id = counter
                service = f"perf-{run_id}-ephemeral"
                instance = f"ephemeral-{worker_id}-{ephemeral_id}"
                lease, sample = client.register(
                    service,
                    instance,
                    f"registration-{run_id}-ephemeral-{worker_id}-{ephemeral_id}",
                )
                if lease:
                    query = urlencode(
                        {
                            "lease_id": lease.lease_id,
                            "operation_id": f"deregister-{run_id}-{worker_id}-{ephemeral_id}",
                        }
                    )
                    delete_status, _, delete_elapsed = client.call(
                        "DELETE",
                        f"/api/v1/services/{quote(service)}/instances/{quote(instance)}?{query}",
                    )
                    with samples_lock:
                        samples.append(Sample("deregister", delete_elapsed, delete_status == 204, delete_status))
            with samples_lock:
                samples.append(sample)

    load_started = time.perf_counter()
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as executor:
        list(executor.map(mixed_worker, range(args.workers)))
    load_seconds = time.perf_counter() - load_started

    operation_samples = samples[preload_sample_count:]
    # Report all operations; registration preload is broken out separately for clarity.
    counts: dict[str, int] = {}
    by_kind: dict[str, list[float]] = {}
    errors = 0
    for sample in samples:
        counts[sample.kind] = counts.get(sample.kind, 0) + 1
        by_kind.setdefault(sample.kind, []).append(sample.elapsed_ms)
        errors += int(not sample.ok)

    total = len(samples)
    result = {
        "config": {
            "base_url": args.base_url,
            "services": args.services,
            "requested_instances": args.instances,
            "registered_instances": len(leases),
            "duration_seconds": args.duration,
            "workers": args.workers,
        },
        "registration_seconds": round(registration_seconds, 3),
        "load_seconds": round(load_seconds, 3),
        "total_operations": total,
        "operations_per_second": round(len(operation_samples) / max(load_seconds, 0.001), 2),
        "error_rate": round(errors / max(total, 1), 6),
        "counts": counts,
        "latency_ms": {
            kind: {
                "p50": round(statistics.median(values), 3),
                "p95": round(percentile(values, 0.95), 3),
                "p99": round(percentile(values, 0.99), 3),
                "max": round(max(values), 3),
            }
            for kind, values in sorted(by_kind.items())
        },
    }
    print(json.dumps(result, ensure_ascii=False, indent=2))

    if args.assert_baseline:
        heartbeat_p95 = result["latency_ms"].get("heartbeat", {}).get("p95", math.inf)
        register_p95 = result["latency_ms"].get("register", {}).get("p95", math.inf)
        discover_p95 = result["latency_ms"].get("discover", {}).get("p95", math.inf)
        passed = (
            len(leases) >= args.instances
            and result["operations_per_second"] >= 3000
            and result["error_rate"] < 0.001
            and heartbeat_p95 <= 10
            and register_p95 <= 20
            and discover_p95 <= 20
        )
        return 0 if passed else 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
