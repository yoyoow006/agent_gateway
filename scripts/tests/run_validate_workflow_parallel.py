#!/usr/bin/env python3
"""零第三方依赖的契约套件并行执行器。

用法:
  python3 -B scripts/tests/run_validate_workflow_parallel.py \
    [--module scripts.tests.test_validate_workflow] [--jobs N]

行为契约:
- 默认 jobs 为 WORKFLOW_TEST_JOBS 或 min(CPU, 8)；--jobs 1 在当前进程按原顺序串行执行。
- 每个用例在独立子进程运行（fork 不可用时进程内串行退化），崩溃折算为该用例 ERROR。
- 输出保持 unittest 的 `... ok/FAIL/ERROR/skipped`、失败回溯与 `Ran N tests` 汇总语义，
  退出码 0/1 与 unittest 一致；`... skipped` 行保持 wrapper 统计兼容。
"""

from __future__ import annotations

import argparse
import importlib
import io
import json
import multiprocessing
import os
import shutil
import sys
import tempfile
import time
import traceback
import unittest
from typing import Any, Iterator


MAX_DEFAULT_JOBS = 8


def default_jobs() -> int:
    configured = os.environ.get("WORKFLOW_TEST_JOBS")
    if configured is not None:
        try:
            return max(1, int(configured))
        except ValueError:
            pass
    return max(1, min(os.cpu_count() or 1, MAX_DEFAULT_JOBS))


def flatten_suite(suite: unittest.TestSuite) -> Iterator[unittest.TestCase]:
    for item in suite:
        if isinstance(item, unittest.TestSuite):
            yield from flatten_suite(item)
        else:
            yield item


def _entry_id(test: Any) -> str:
    try:
        return test.id()
    except Exception:
        return str(test)


def summarize_result(
    test: unittest.TestCase, result: unittest.TestResult
) -> dict[str, Any]:
    failures = [
        {"id": _entry_id(entry), "traceback": traceback_text}
        for entry, traceback_text in result.failures
    ]
    errors = [
        {"id": _entry_id(entry), "traceback": traceback_text}
        for entry, traceback_text in result.errors
    ]
    skipped = [
        {"id": _entry_id(entry), "reason": reason}
        for entry, reason in result.skipped
    ]
    expected_failures = [
        {"id": _entry_id(entry)} for entry, _ in result.expectedFailures
    ]
    unexpected_successes = [
        {"id": _entry_id(entry)} for entry in result.unexpectedSuccesses
    ]
    if errors:
        outcome = "ERROR"
    elif failures or unexpected_successes:
        outcome = "FAIL"
    elif skipped:
        outcome = "skipped"
    else:
        outcome = "ok"
    return {
        "id": test.id(),
        "outcome": outcome,
        "failures": failures,
        "errors": errors,
        "skipped": skipped,
        "expected_failures": expected_failures,
        "unexpected_successes": unexpected_successes,
        "stdout": "",
        "stderr": "",
    }


def run_test_in_process(test: unittest.TestCase) -> dict[str, Any]:
    stdout = io.StringIO()
    stderr = io.StringIO()
    previous_stdout, previous_stderr = sys.stdout, sys.stderr
    sys.stdout, sys.stderr = stdout, stderr
    try:
        result = unittest.TestResult()
        test.run(result)
        payload = summarize_result(test, result)
    except BaseException:
        payload = {
            "id": test.id(),
            "outcome": "ERROR",
            "failures": [],
            "expected_failures": [],
            "errors": [
                {
                    "id": test.id(),
                    "traceback": traceback.format_exc(),
                }
            ],
            "skipped": [],
            "unexpected_successes": [],
            "stdout": stdout.getvalue(),
            "stderr": stderr.getvalue(),
        }
        return payload
    finally:
        sys.stdout, sys.stderr = previous_stdout, previous_stderr
    payload["stdout"] = stdout.getvalue()
    payload["stderr"] = stderr.getvalue()
    return payload


def child_worker(test: unittest.TestCase, result_path: str) -> None:
    """在独立进程中执行单个用例并把 JSON 结果写盘后立即退出。"""
    exit_code = 0
    try:
        payload = run_test_in_process(test)
        with open(result_path, "w", encoding="utf-8") as destination:
            json.dump(payload, destination)
    except BaseException:
        exit_code = 1
    os._exit(exit_code)


def _load_child_result(
    process: multiprocessing.process.BaseProcess,
    result_path: str,
    test: unittest.TestCase,
) -> dict[str, Any]:
    payload: dict[str, Any] | None = None
    try:
        with open(result_path, encoding="utf-8") as source:
            loaded = json.load(source)
        if isinstance(loaded, dict):
            payload = loaded
    except (OSError, ValueError):
        payload = None
    if payload is None or process.exitcode != 0:
        detail = f"worker exited with code {process.exitcode}; result unavailable"
        return {
            "id": test.id(),
            "outcome": "ERROR",
            "failures": [],
            "errors": [{"id": test.id(), "traceback": detail}],
            "skipped": [],
            "expected_failures": [],
            "unexpected_successes": [],
            "stdout": "",
            "stderr": "",
        }
    return payload


def run_parallel(
    tests: list[unittest.TestCase], jobs: int
) -> list[dict[str, Any]]:
    if "fork" not in multiprocessing.get_all_start_methods():
        return [run_test_in_process(test) for test in tests]
    context = multiprocessing.get_context("fork")
    results: dict[int, dict[str, Any]] = {}
    pending = list(enumerate(tests))
    running: dict[
        multiprocessing.process.BaseProcess,
        tuple[int, str, unittest.TestCase],
    ] = {}
    workspace = tempfile.mkdtemp(prefix="workflow-parallel-")
    try:
        while pending or running:
            while pending and len(running) < jobs:
                index, test = pending.pop(0)
                descriptor, result_path = tempfile.mkstemp(
                    dir=workspace, suffix=".json"
                )
                os.close(descriptor)
                process = context.Process(
                    target=child_worker, args=(test, result_path)
                )
                process.start()
                running[process] = (index, result_path, test)
            for process in list(running):
                process.join(timeout=0.02)
                if process.exitcode is not None:
                    index, result_path, test = running.pop(process)
                    results[index] = _load_child_result(
                        process, result_path, test
                    )
    finally:
        for process in running:
            process.terminate()
            process.join()
        shutil.rmtree(workspace, ignore_errors=True)
    return [results[index] for index in range(len(tests))]


def _write_stream(text: str, stream: Any) -> None:
    if not text:
        return
    if not text.endswith("\n"):
        text += "\n"
    stream.write(text)


def print_report(payloads: list[dict[str, Any]], wall_seconds: float) -> int:
    failure_entries = 0
    error_entries = 0
    skipped_entries = 0
    expected_failure_entries = 0
    unexpected_success_entries = 0
    blocks: list[str] = []
    for payload in payloads:
        status_line = f"{payload['id']} ... {payload['outcome']}"
        if payload["outcome"] == "skipped" and payload["skipped"]:
            status_line += f" '{payload['skipped'][0]['reason']}'"
        print(status_line)
        _write_stream(payload.get("stdout", ""), sys.stdout)
        _write_stream(payload.get("stderr", ""), sys.stderr)
        for kind, entries in (
            ("FAIL", payload["failures"]),
            ("ERROR", payload["errors"]),
            ("EXPECTED FAILURE", payload["expected_failures"]),
            ("UNEXPECTEDLY SUCCEEDED", payload["unexpected_successes"]),
        ):
            for entry in entries:
                detail = entry.get("traceback", "test unexpectedly passed")
                blocks.append(
                    f"{'=' * 70}\n"
                    f"{kind}: {entry['id']}\n"
                    f"{'-' * 70}\n"
                    f"{detail}"
                )
        failure_entries += len(payload["failures"])
        error_entries += len(payload["errors"])
        skipped_entries += len(payload["skipped"])
        expected_failure_entries += len(payload["expected_failures"])
        unexpected_success_entries += len(payload["unexpected_successes"])
    for block in blocks:
        print(block)
    print(f"Ran {len(payloads)} tests in {wall_seconds:.3f}s")
    print()
    summary_parts = []
    if failure_entries:
        summary_parts.append(f"failures={failure_entries}")
    if error_entries:
        summary_parts.append(f"errors={error_entries}")
    if skipped_entries:
        summary_parts.append(f"skipped={skipped_entries}")
    if expected_failure_entries:
        summary_parts.append(f"expected failures={expected_failure_entries}")
    if unexpected_success_entries:
        summary_parts.append(f"unexpected successes={unexpected_success_entries}")
    if failure_entries or error_entries or unexpected_success_entries:
        print(f"FAILED ({', '.join(summary_parts)})")
        return 1
    if skipped_entries or expected_failure_entries:
        print(
            "OK ("
            + ", ".join(
                part
                for part in (
                    f"skipped={skipped_entries}" if skipped_entries else "",
                    f"expected failures={expected_failure_entries}"
                    if expected_failure_entries
                    else "",
                )
                if part
            )
            + ")"
        )
    else:
        print("OK")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--module",
        default="scripts.tests.test_validate_workflow",
        help="要并行执行的 unittest 模块",
    )
    parser.add_argument(
        "--jobs",
        type=int,
        default=default_jobs(),
        help="并行进程数；1 表示进程内按原顺序串行",
    )
    arguments = parser.parse_args(argv)
    jobs = max(1, arguments.jobs)
    sys.path.insert(0, os.getcwd())
    module = importlib.import_module(arguments.module)
    suite = unittest.defaultTestLoader.loadTestsFromModule(module)
    tests = list(flatten_suite(suite))
    if not tests:
        print("Ran 0 tests")
        print()
        print("OK")
        return 0
    started = time.perf_counter()
    if jobs == 1:
        payloads = [run_test_in_process(test) for test in tests]
    else:
        payloads = run_parallel(tests, jobs)
    wall_seconds = time.perf_counter() - started
    return print_report(payloads, wall_seconds)


if __name__ == "__main__":
    sys.exit(main())
