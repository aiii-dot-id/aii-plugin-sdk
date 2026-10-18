# Copyright AI Identity Incorporated. SPDX-License-Identifier: Apache-2.0
"""Hermetic parser and external-verifier boundary tests; no signing keys."""

import argparse
import contextlib
import hashlib
import importlib.util
import io
import json
import os
import pathlib
import ssl
import sys
import tempfile
import textwrap
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location("verify_sdk_release", pathlib.Path(__file__).with_name("verify-sdk-release.py"))
VERIFY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VERIFY)
ROOT_DER = b"\x30\x03\x02\x01\x01"
SIGNER = "a1" * 32
STATUS_LINES = (
    "Signature verification: ok",
    "Timestamp Server Signature verification: ok",
    "Timestamp Server Signature CRL verification: ok",
    "Signature CRL verification: ok",
)


def tlv(tag, data):
    if len(data) < 128:
        length = bytes([len(data)])
    else:
        size = (len(data).bit_length() + 7) // 8
        length = bytes([128 + size]) + len(data).to_bytes(size, "big")
    return bytes([tag]) + length + data


def sequence(*values):
    return tlv(0x30, b"".join(values))


def oid(value):
    return tlv(6, value)


def attribute(kind, value):
    return sequence(oid(kind), tlv(0x31, value))


def member(manifest, *, sha256=True, filename="SHA256SUMS\x00", digest=None, duplicate_name=False, extra_attribute=False):
    name = attribute(VERIFY.NAME_VALUE, sequence(
        tlv(0x1E, "Filename".encode("utf-16-be")),
        tlv(2, bytes.fromhex("10010001")),
        tlv(4, filename.encode("utf-16-le"))))
    attributes = [name, attribute(VERIFY.MEMBER_INFO, tlv(0x82, b""))]
    reference = hashlib.sha256(manifest).digest() if sha256 else hashlib.sha1(manifest).digest()
    if sha256:
        attributes.append(attribute(VERIFY.INDIRECT_DATA, sequence(
            sequence(oid(VERIFY.FLAT_FILE), tlv(0xA2, b"\x80\x00")),
            sequence(sequence(oid(VERIFY.SHA256), tlv(5, b"")), tlv(4, digest if digest is not None else reference)))))
    if duplicate_name:
        attributes.append(name)
    if extra_attribute:
        attributes.append(attribute(b"\x2a\x03", tlv(5, b"")))
    return sequence(tlv(4, reference), tlv(0x31, b"".join(attributes)))


def catalog(manifest, *, members=None, filename="SHA256SUMS\x00", **member_options):
    if members is None:
        members = [member(manifest, sha256=False, filename=filename),
                   member(manifest, filename=filename, **member_options)]
    ctl = sequence(
        sequence(oid(bytes.fromhex("2b0601040182370c0101"))),
        tlv(4, b"0" * 16), tlv(0x17, b"261018000000Z"),
        sequence(oid(bytes.fromhex("2b0601040182370c0103")), tlv(5, b"")),
        sequence(*members))
    content = sequence(oid(VERIFY.CATALOG), tlv(0xA0, ctl))
    signed = sequence(tlv(2, b"\x01"), tlv(0x31, b""), content, tlv(0x31, b""))
    # Deliberately unsigned: these fixtures test membership, never authenticity.
    return sequence(oid(VERIFY.SIGNED_DATA), tlv(0xA0, signed))


class CatalogTests(unittest.TestCase):
    def setUp(self):
        self.manifest = ("12" * 32 + "  sdk.zip\n").encode("ascii")

    def test_exact_makecat_v2_member(self):
        VERIFY.catalog_member(catalog(self.manifest), self.manifest)

    def test_changed_manifest_is_not_a_member(self):
        with self.assertRaisesRegex(ValueError, "reference|digest"):
            VERIFY.catalog_member(catalog(self.manifest), self.manifest + b"\n")

    def test_exact_authenticated_filename_required(self):
        for filename in ("OTHER\x00", "SHA256SUMS", "../SHA256SUMS\x00", "SHA256SUMS\x00extra"):
            with self.subTest(filename=filename), self.assertRaisesRegex(ValueError, "filename"):
                VERIFY.catalog_member(catalog(self.manifest, filename=filename), self.manifest)

    def test_digest_and_reference_both_required(self):
        with self.assertRaisesRegex(ValueError, "digest"):
            VERIFY.catalog_member(catalog(self.manifest, digest=b"x" * 32), self.manifest)

    def test_duplicate_members_names_and_unknown_attributes_refused(self):
        cases = (
            catalog(self.manifest, members=[member(self.manifest), member(self.manifest)]),
            catalog(self.manifest, duplicate_name=True),
            catalog(self.manifest, extra_attribute=True),
            catalog(self.manifest, members=[member(self.manifest)]),
        )
        for candidate in cases:
            with self.subTest(candidate=candidate[-30:]), self.assertRaises(ValueError):
                VERIFY.catalog_member(candidate, self.manifest)

    def test_unparsed_digest_injected_in_wrong_shape_is_not_membership(self):
        with self.assertRaises(ValueError):
            VERIFY.catalog_member(tlv(4, hashlib.sha256(self.manifest).digest()), self.manifest)

    def test_der_malformed_lengths_trailing_data_and_field_flood_refused(self):
        for malformed in (b"\x30", b"\x30\x80\x00\x00", b"\x30\x81\x01x",
                          b"\x30\x82\x00\x80" + b"x" * 128, b"\x30\x02x",
                          b"\x1f\x00", b"\x05\x00" * 17):
            with self.subTest(malformed=malformed[:8]), self.assertRaises(ValueError):
                VERIFY.der(malformed)
        with self.assertRaisesRegex(ValueError, "one DER"):
            VERIFY.catalog_member(catalog(self.manifest) + b"\x05\x00", self.manifest)


class VerifierTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="sdk-verifier-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.artifact = self.root / "sdk.zip"
        self.artifact.write_bytes(b"downloaded SDK\x00\xff")
        self.second = self.root / "sdk.tar.gz"
        self.second.write_bytes(b"second download")
        self.manifest = self.root / "SHA256SUMS"
        self.set_manifest((self.artifact, self.second))
        self.ca = self.root / "root.pem"
        self.ca.write_text("test root plus an untrusted appended certificate", encoding="ascii")
        self.log = self.root / "calls.jsonl"
        self.tool = self.root / "tool.py"
        # Both real subprocess boundaries execute this controlled tool. There is
        # no catalog signature in these fixtures; production cryptography stays
        # entirely in the installed verifier.
        self.tool.write_text(textwrap.dedent("""\
            import json, os, pathlib, ssl, sys
            root = bytes.fromhex(os.environ['TEST_ROOT_DER'])
            with open(os.environ['TEST_TOOL_LOG'], 'a', encoding='utf-8') as log:
                log.write(json.dumps(sys.argv[1:]) + '\\n')
            if sys.argv[1] == 'x509':
                if os.environ.get('TEST_OPENSSL_FAIL'):
                    sys.exit(2)
                sys.stdout.buffer.write(b'wrong-root' if os.environ.get('TEST_BAD_ROOT') else root)
            else:
                if os.environ.get('TEST_SIGNATURE_FAIL'):
                    sys.exit(3)
                expected = 'sha256:' + os.environ['TEST_SIGNER']
                if sys.argv[sys.argv.index('-require-leaf-hash') + 1] != expected:
                    sys.exit(4)
                ca = pathlib.Path(sys.argv[sys.argv.index('-CAfile') + 1])
                tsa = pathlib.Path(sys.argv[sys.argv.index('-TSA-CAfile') + 1])
                if ca.read_text() != ssl.DER_cert_to_PEM_cert(root) or tsa != ca:
                    sys.exit(5)
                for line in json.loads(os.environ['TEST_STATUSES']):
                    if line != os.environ.get('TEST_MISSING_STATUS'):
                        print(line)
            """), encoding="utf-8")
        # Run the controlled tool with this interpreter, rather than relying on
        # executable-script handling that CreateProcess does not provide.
        real_run = VERIFY.run
        self.runner = mock.patch.object(VERIFY, "run", side_effect=lambda command, **kwargs:
                                        real_run([sys.executable] + command, **kwargs))
        self.runner.start()
        self.addCleanup(self.runner.stop)
        self.environment = mock.patch.dict(os.environ, {
            "TEST_ROOT_DER": ROOT_DER.hex(), "TEST_TOOL_LOG": str(self.log),
            "TEST_SIGNER": SIGNER, "TEST_STATUSES": json.dumps(STATUS_LINES),
        })
        self.environment.start()
        self.addCleanup(self.environment.stop)
        self.pin = mock.patch.object(VERIFY, "ROOT_SHA256", hashlib.sha256(ROOT_DER).hexdigest())
        self.pin.start()
        self.addCleanup(self.pin.stop)
        self.args = argparse.Namespace(manifest=str(self.manifest), catalog=str(self.cat),
                                       ca_file=str(self.ca), signer_sha256=SIGNER.upper(),
                                       openssl=str(self.tool), osslsigncode=str(self.tool),
                                       artifact=[str(self.artifact), str(self.second)])

    def set_manifest(self, artifacts):
        self.manifest.write_text("".join(hashlib.sha256(path.read_bytes()).hexdigest() + "  " + path.name + "\n" for path in artifacts), encoding="ascii")
        self.cat = self.root / "SHA256SUMS.cat"
        self.cat.write_bytes(catalog(self.manifest.read_bytes()))

    def run_verify(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            VERIFY.verify(self.args)
        return output.getvalue()

    def assert_refused(self, text):
        output = io.StringIO()
        with contextlib.redirect_stdout(output), self.assertRaisesRegex((ValueError, OSError), text):
            VERIFY.verify(self.args)
        self.assertEqual(output.getvalue(), "", "partial verification was reported before refusal")

    def test_full_verification_pins_both_chains_and_signer_without_mutating_downloads(self):
        paths = (self.ca, self.manifest, self.cat, self.artifact, self.second)
        before = {path: (path.read_bytes(), path.stat().st_mode) for path in paths}
        self.assertEqual(self.run_verify(), "VERIFIED sdk.zip\nVERIFIED sdk.tar.gz\n")
        self.assertEqual(before, {path: (path.read_bytes(), path.stat().st_mode) for path in paths})
        calls = [json.loads(line) for line in self.log.read_text().splitlines()]
        self.assertEqual(len(calls), 2)
        scratch_root = calls[0][2]
        self.assertEqual(calls[0], ["x509", "-in", scratch_root, "-outform", "DER"])
        self.assertEqual(calls[1], ["verify", "-CAfile", scratch_root, "-TSA-CAfile", scratch_root,
                                   "-require-leaf-hash", "sha256:" + SIGNER,
                                   "-in", str(pathlib.Path(scratch_root).with_name("SHA256SUMS.cat"))])

    def test_crypto_failure_and_wrong_root_or_published_signer_refused(self):
        for variable, message in (("TEST_OPENSSL_FAIL", "command failed"),
                                  ("TEST_BAD_ROOT", "wrong Microsoft trust anchor"),
                                  ("TEST_SIGNATURE_FAIL", "command failed")):
            with self.subTest(variable=variable), mock.patch.dict(os.environ, {variable: "1"}):
                self.assert_refused(message)
        self.args.signer_sha256 = "b2" * 32
        self.assert_refused("command failed")
        self.args.signer_sha256 = "not-a-fingerprint"
        self.assert_refused("published SHA-256 signer")

    def test_each_signature_timestamp_and_revocation_status_required(self):
        for status in STATUS_LINES:
            with self.subTest(status=status), mock.patch.dict(os.environ, {"TEST_MISSING_STATUS": status}):
                self.assert_refused("required")

    def test_success_status_must_be_whole_line(self):
        statuses = ["not " + line for line in STATUS_LINES]
        with mock.patch.dict(os.environ, {"TEST_STATUSES": json.dumps(statuses)}):
            self.assert_refused("verified signature")

    def test_catalog_or_late_artifact_tampering_reports_no_partial_success(self):
        self.second.write_bytes(b"changed second download")
        self.assert_refused("checksum mismatch: sdk.tar.gz")
        self.cat.write_bytes(catalog(self.manifest.read_bytes(), digest=b"x" * 32))
        self.assert_refused("digest")

    def test_absent_or_duplicate_requested_artifact_refused(self):
        self.args.artifact.append(str(self.root / "absent.zip"))
        self.assert_refused("absent from the signed manifest")
        self.args.artifact = [str(self.artifact), str(self.artifact)]
        self.assert_refused("duplicate requested")

    def test_empty_invalid_duplicate_and_unsafe_manifest_lines_refused(self):
        for content in (b"", b"not a checksum\n", b"ff" * 32 + b"  ../sdk.zip\n",
                        b"ff" * 32 + b"  .\n", b"ff" * 32 + b"  ..\n",
                        self.manifest.read_bytes() * 2, b"\xff\n"):
            self.manifest.write_bytes(content)
            self.cat.write_bytes(catalog(content))
            with self.subTest(content=content[:20]):
                self.assert_refused("manifest|filename|ascii")

    def test_wrong_manifest_name_and_oversized_metadata_refused(self):
        self.args.manifest = str(self.root / "wrong-name")
        self.assert_refused("must be named SHA256SUMS")
        self.args.manifest = str(self.manifest)
        self.manifest.write_bytes(b"x" * 65537)
        self.assert_refused("metadata is too large")
        self.manifest.write_bytes(b"")
        self.cat.write_bytes(b"x" * (4 * 1024 * 1024 + 1))
        self.assert_refused("metadata is too large")

    def test_cli_failure_is_clean_and_nonzero(self):
        arguments = ["verify-sdk-release.py", "--ca-file", str(self.ca),
                     "--signer-sha256", "invalid", "--manifest", str(self.manifest),
                     "--catalog", str(self.cat), str(self.artifact)]
        stdout, stderr = io.StringIO(), io.StringIO()
        with mock.patch.object(sys, "argv", arguments), contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            self.assertEqual(VERIFY.main(), 1)
        self.assertEqual(stdout.getvalue(), "")
        self.assertTrue(stderr.getvalue().startswith("NOT VERIFIED: "))
        self.assertNotIn("Traceback", stderr.getvalue())

    def test_changed_file_metadata_refused(self):
        before = self.artifact.stat()
        after = mock.Mock(st_size=before.st_size, st_mtime_ns=before.st_mtime_ns + 1,
                          st_ctime_ns=before.st_ctime_ns)
        with mock.patch.object(VERIFY.os, "fstat", side_effect=[before, before, after]):
            with self.assertRaisesRegex(ValueError, "artifact changed"):
                VERIFY.checksum(self.artifact)

    @unittest.skipUnless(hasattr(os, "mkfifo"), "FIFO input is POSIX-specific")
    def test_fifo_refused_without_waiting_for_writer(self):
        fifo = self.root / "fifo"
        os.mkfifo(fifo)
        with self.assertRaisesRegex(ValueError, "not a regular file"):
            VERIFY.read_limited(fifo, 1024)


class ToolBoundsTests(unittest.TestCase):
    def test_output_size_and_execution_time_are_bounded(self):
        with self.assertRaisesRegex(ValueError, "output is too large"):
            VERIFY.run([sys.executable, "-c", "import sys; sys.stdout.write('x' * 8192)"], limit=1024)
        with self.assertRaisesRegex(ValueError, "timed out"):
            VERIFY.run([sys.executable, "-c", "import time; time.sleep(60)"], timeout=0.05)

    def test_failed_tool_reports_stderr(self):
        with self.assertRaisesRegex(ValueError, "refused"):
            VERIFY.run([sys.executable, "-c", "import sys; print('refused', file=sys.stderr); sys.exit(2)"])


if __name__ == "__main__":
    unittest.main()
