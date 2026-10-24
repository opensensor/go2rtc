#!/usr/bin/env python3
"""Fail on new or changed test failures; report pre-existing upstream failures."""
import json
from pathlib import Path
import re
import subprocess
import tempfile

BASE = 'a5304e5'
root = Path.cwd()


def run_tests(directory, output):
    with output.open('w') as stream:
        result = subprocess.run(['go', 'test', '-json', '-timeout', '120s', './...'], cwd=directory, stdout=stream)
    failures = set()
    messages = {}
    for line in output.read_text().splitlines():
        event = json.loads(line)
        key = (event.get('Package'), event.get('Test'))
        if event['Action'] == 'fail':
            failures.add(key)
        if event['Action'] == 'output' and event.get('Test'):
            text = event['Output'].replace(str(directory), '<source>')
            text = re.sub(r'\(\d+(?:\.\d+)?s\)', '(elapsed)', text)
            if not text.startswith(('=== RUN', '=== PAUSE', '=== CONT', '--- PASS')):
                messages.setdefault(key, []).append(text)
    if result.returncode and not failures:
        raise RuntimeError('Test infrastructure failed without structured failure evidence')
    return {key: ''.join(messages.get(key, [])) for key in failures}


with tempfile.TemporaryDirectory(prefix='go2rtc-test-baseline-') as tmp:
    baseline = Path(tmp) / 'source'
    subprocess.run(['git', 'worktree', 'add', '--detach', str(baseline), BASE], check=True)
    try:
        before = run_tests(baseline, root / 'baseline-tests.jsonl')
        after = run_tests(root, root / 'candidate-tests.jsonl')
        changed = [key for key, message in after.items() if key not in before or before[key] != message]
        print(json.dumps({'baseline_failures': len(before), 'candidate_failures': len(after),
                          'new_or_changed_failures': changed}))
        if changed:
            raise SystemExit(1)
        if before:
            print('::warning::Existing upstream test failures reproduced unchanged; see both test artifacts.')
    finally:
        subprocess.run(['git', 'worktree', 'remove', '--force', str(baseline)], check=True)
