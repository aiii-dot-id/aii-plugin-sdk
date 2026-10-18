#!/usr/bin/env python3
import argparse
import hashlib
import os
import pathlib
import re
import ssl
import stat
import subprocess
import sys
import tempfile


ROOT_SHA256 = "5367f20c7ade0e2bca790915056d086b720c33c1fa2a2661acf787e3292e1270"
SIGNED_DATA = bytes.fromhex("2a864886f70d010702")
CATALOG = bytes.fromhex("2b0601040182370a01")
NAME_VALUE = bytes.fromhex("2b0601040182370c0201")
MEMBER_INFO = bytes.fromhex("2b0601040182370c0203")
INDIRECT_DATA = bytes.fromhex("2b060104018237020104")
FLAT_FILE = bytes.fromhex("2b060104018237020119")
SHA256 = bytes.fromhex("608648016503040201")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def der(data):
    fields = []
    while data:
        require(len(fields) < 16, "too many DER fields")
        require(len(data) >= 2, "truncated DER field")
        tag, size = data[:2]
        require(tag & 31 != 31, "unsupported DER tag")
        offset = 2
        if size & 128:
            count = size & 127
            require(0 < count <= 4 and len(data) >= 2 + count, "invalid DER length")
            require(data[2] != 0, "nonminimal DER length")
            size = int.from_bytes(data[2:2 + count], "big")
            require(size >= 128, "nonminimal DER length")
            offset += count
        require(size <= len(data) - offset, "truncated DER value")
        fields.append((tag, data[offset:offset + size]))
        data = data[offset + size:]
    return fields


def value(field, tag):
    require(field[0] == tag, "unexpected DER field type")
    return field[1]


def children(field, tag=0x30, count=None):
    result = der(value(field, tag))
    require(count is None or len(result) == count, "unexpected DER field count")
    return result


def only(data):
    result = der(data)
    require(len(result) == 1, "expected one DER value")
    return result[0]


def catalog_member(catalog, manifest):
    outer = children(only(catalog), count=2)
    require(value(outer[0], 6) == SIGNED_DATA, "expected PKCS#7 SignedData")
    signed = children(only(value(outer[1], 0xA0)))
    require(len(signed) >= 4 and value(signed[0], 2) == b"\x01", "unsupported SignedData")
    content = children(signed[2], count=2)
    require(value(content[0], 6) == CATALOG, "expected Microsoft catalog content")
    ctl = children(only(value(content[1], 0xA0)), count=5)
    usage = children(ctl[0], count=1)
    require(value(usage[0], 6) == bytes.fromhex("2b0601040182370c0101"), "unsupported catalog usage")
    require(len(value(ctl[1], 4)) == 16, "invalid catalog identifier")
    value(ctl[2], 0x17)
    version = children(ctl[3], count=2)
    require(value(version[0], 6) == bytes.fromhex("2b0601040182370c0103") and version[1] == (5, b""), "unsupported catalog version")
    members = children(ctl[4], count=2)
    expected = hashlib.sha256(manifest).digest()
    references = set()
    verified = 0
    for member in members:
        reference, attributes = children(member, count=2)
        reference = value(reference, 4)
        require(len(reference) in (20, 32) and len(reference) not in references, "duplicate or unsupported catalog member")
        references.add(len(reference))
        names, digests, infos = [], [], 0
        for attribute in children(attributes, 0x31):
            oid, values = children(attribute, count=2)
            oid = value(oid, 6)
            entry = children(values, 0x31, 1)[0]
            if oid == NAME_VALUE:
                name, flags, text = children(entry, count=3)
                require(value(name, 0x1E).decode("utf-16-be") == "Filename", "unexpected member attribute")
                require(value(flags, 2) == bytes.fromhex("10010001"), "filename attribute is not authenticated text")
                names.append(value(text, 4).decode("utf-16-le"))
            elif oid == MEMBER_INFO:
                require(entry == (0x82, b""), "unsupported member information")
                infos += 1
            elif oid == INDIRECT_DATA:
                kind, digest = children(entry, count=2)
                kind = children(kind, count=2)
                require(value(kind[0], 6) == FLAT_FILE and kind[1] == (0xA2, b"\x80\x00"), "expected flat-file digest")
                algorithm, digest = children(digest, count=2)
                algorithm = children(algorithm, count=2)
                require(value(algorithm[0], 6) == SHA256 and algorithm[1] == (5, b""), "expected SHA-256")
                digests.append(value(digest, 4))
            else:
                raise ValueError("unsupported catalog member attribute")
        require(names == ["SHA256SUMS\x00"] and infos == 1, "catalog must authenticate exactly the SHA256SUMS filename")
        if len(reference) == 32:
            require(reference == expected and digests == [expected], "SHA256SUMS digest does not match catalog member")
            verified += 1
        else:
            require(reference == hashlib.sha1(manifest).digest() and not digests, "inconsistent legacy catalog reference")
    require(verified == 1, "expected exactly one SHA-256 member")


def run(command, timeout=120, limit=1024 * 1024):
    with tempfile.TemporaryFile() as capture:
        try:
            result = subprocess.run(command, stdin=subprocess.DEVNULL, stdout=capture,
                                    stderr=subprocess.STDOUT, timeout=timeout)
        except subprocess.TimeoutExpired:
            raise ValueError("command timed out: " + command[0]) from None
        capture.seek(0)
        output = capture.read(limit + 1)
        require(len(output) <= limit, "command output is too large: " + command[0])
        require(result.returncode == 0, "command failed: " + command[0] + "\n" + output.decode("utf-8", errors="replace"))
        return output


def open_regular(path):
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NONBLOCK", 0))
    try:
        require(stat.S_ISREG(os.fstat(descriptor).st_mode), "release input is not a regular file: " + str(path))
        return os.fdopen(descriptor, "rb")
    except BaseException:
        os.close(descriptor)
        raise


def read_limited(path, limit):
    with open_regular(path) as stream:
        data = stream.read(limit + 1)
    require(len(data) <= limit, "release metadata is too large")
    return data


def checksum(path):
    with open_regular(path) as stream:
        before = os.fstat(stream.fileno())
        digest = hashlib.sha256()
        remaining = before.st_size
        while remaining:
            block = stream.read(min(remaining, 1024 * 1024))
            require(block, "artifact changed during verification: " + str(path))
            digest.update(block)
            remaining -= len(block)
        require(not stream.read(1), "artifact changed during verification: " + str(path))
        after = os.fstat(stream.fileno())
    require((before.st_size, before.st_mtime_ns, before.st_ctime_ns) ==
            (after.st_size, after.st_mtime_ns, after.st_ctime_ns),
            "artifact changed during verification: " + str(path))
    return digest.hexdigest()


def verify(args):
    manifest_path = pathlib.Path(args.manifest)
    require(manifest_path.name == "SHA256SUMS", "manifest must be named SHA256SUMS")
    catalog = read_limited(args.catalog, 4 * 1024 * 1024)
    manifest = read_limited(manifest_path, 65536)
    require(re.fullmatch(r"[0-9a-fA-F]{64}", args.signer_sha256), "provide the release's published SHA-256 signer fingerprint")
    with tempfile.TemporaryDirectory(prefix="sdk-verify-") as scratch:
        scratch = pathlib.Path(scratch)
        ca = scratch / "root.pem"
        ca.write_bytes(read_limited(args.ca_file, 65536))
        root_der = run([args.openssl, "x509", "-in", str(ca), "-outform", "DER"], timeout=30, limit=65536)
        require(hashlib.sha256(root_der).hexdigest() == ROOT_SHA256, "wrong Microsoft trust anchor")
        ca.write_text(ssl.DER_cert_to_PEM_cert(root_der), encoding="ascii")
        copy = scratch / "SHA256SUMS.cat"
        copy.write_bytes(catalog)
        output = run([args.osslsigncode, "verify", "-CAfile", str(ca), "-TSA-CAfile", str(ca),
                      "-require-leaf-hash", "sha256:" + args.signer_sha256.lower(), "-in", str(copy)])
        statuses = set(output.decode("utf-8", errors="replace").splitlines())
        require("Signature verification: ok" in statuses, "a verified signature is required")
        require("Timestamp Server Signature verification: ok" in statuses, "a verified timestamp is required")
        require({"Timestamp Server Signature CRL verification: ok", "Signature CRL verification: ok"} <= statuses,
                "successful signer and timestamp revocation checks are required")
    catalog_member(catalog, manifest)
    entries = {}
    for line in manifest.decode("ascii").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([A-Za-z0-9_.-]+)", line)
        require(match is not None, "invalid checksum manifest line")
        digest, name = match.groups()
        require(name not in entries and name not in (".", ".."), "duplicate or unsafe manifest filename")
        entries[name] = digest
    require(entries, "empty checksum manifest")
    verified = []
    for filename in args.artifact:
        artifact = pathlib.Path(filename)
        require(artifact.name in entries, "artifact is absent from the signed manifest: " + artifact.name)
        require(artifact.name not in verified, "duplicate requested artifact: " + artifact.name)
        require(checksum(artifact) == entries[artifact.name], "artifact checksum mismatch: " + artifact.name)
        verified.append(artifact.name)
    for name in verified:
        print("VERIFIED " + name)


def main():
    parser = argparse.ArgumentParser(description="Verify SDK downloads against a timestamped Authenticode checksum catalog.")
    parser.add_argument("--ca-file", required=True, help="Microsoft Identity Verification Root 2020 certificate in PEM format")
    parser.add_argument("--signer-sha256", required=True, help="signer certificate fingerprint from the release notes")
    parser.add_argument("--manifest", default="SHA256SUMS")
    parser.add_argument("--catalog", default="SHA256SUMS.cat")
    parser.add_argument("--osslsigncode", default="osslsigncode")
    parser.add_argument("--openssl", default="openssl")
    parser.add_argument("artifact", nargs="+", help="downloaded release files to verify")
    args = parser.parse_args()
    try:
        verify(args)
    except (ValueError, OSError, subprocess.SubprocessError, UnicodeError) as error:
        print("NOT VERIFIED: " + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
