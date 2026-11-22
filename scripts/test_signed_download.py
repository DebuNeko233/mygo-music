#!/usr/bin/env python3
"""Exercise audio tagging and decoding under macOS Hardened Runtime."""

import argparse
import json
import os
from pathlib import Path
import plistlib
import re
import subprocess
import sys
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--identity", default="-", help="codesign identity; defaults to ad hoc")
    parser.add_argument("--output", type=Path, help="keep verification logs here")
    args = parser.parse_args()
    if sys.platform != "darwin":
        parser.error("requires macOS code signing and runtime enforcement")

    repo = Path(__file__).resolve().parent.parent
    config = json.loads((repo / "mygo.json").read_text())
    entitlements = repo / config["macos"]["entitlements"]
    expected_entitlements = plistlib.loads(entitlements.read_bytes())
    report = []

    def run(name, command, *, cwd=repo, env=None):
        result = subprocess.run(command, cwd=cwd, env=env, capture_output=True, text=True, timeout=180)
        if args.output:
            args.output.mkdir(parents=True, exist_ok=True)
            (args.output / (name + ".log")).write_text(result.stdout + result.stderr)
        report.append({"name": name, "exit_code": result.returncode})
        print(name + ": " + str(result.returncode), flush=True)
        if result.returncode != 0:
            print(result.stdout + result.stderr, file=sys.stderr)
            raise RuntimeError(name + " failed")
        return result

    # The first release passed unsigned tests and static signature checks but
    # died when TagLib's generated machine code first executed in a signed app.
    # Each suite runs in separate signed processes against cold and warm caches.
    with tempfile.TemporaryDirectory(prefix="mygo-signed-download-") as temp:
        root = Path(temp)
        for package, tests in [
            ("download", "^TestSave"),
            ("player", "^(TestTaggedFormatsPreservePCM|TestCopyAudioCacheAndNetwork|TestCopyAudioCancellation)$"),
        ]:
            binary = root / (package + ".test")
            run("build-" + package, ["go", "test", "-c", "-o", str(binary), "./internal/" + package],
                env=os.environ | {"CGO_ENABLED": "0"})
            run("sign-" + package, ["codesign", "--force", "--sign", args.identity,
                "--options", "runtime", "--timestamp=none", "--entitlements", str(entitlements), str(binary)])
            run("verify-" + package, ["codesign", "--verify", "--strict", str(binary)])
            signature = run("signature-" + package, ["codesign", "-dv", "--verbose=4", str(binary)])
            if not re.search(r"flags=0x[0-9a-fA-F]+\([^)]*\bruntime\b", signature.stderr):
                raise RuntimeError("test executable is not hardened")
            signed = run("entitlements-" + package, ["codesign", "-d", "--entitlements", "-", "--xml", str(binary)])
            if plistlib.loads(signed.stdout.encode()) != expected_entitlements:
                raise RuntimeError("signed entitlements differ from the application configuration")
            cache = root / (package + "-cache")
            cache.mkdir()
            for state in ("cold", "warm"):
                run(package + "-" + state,
                    [str(binary), "-test.run", tests, "-test.v", "-test.timeout", "120s"],
                    cwd=repo / "internal" / package, env=os.environ | {"TMPDIR": str(cache)})
    if args.output:
        (args.output / "results.json").write_text(json.dumps(report, indent=2) + "\n")
    print("Signed download and playback regressions passed.")


if __name__ == "__main__":
    main()
