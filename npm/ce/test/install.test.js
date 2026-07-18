"use strict";

const assert = require("assert/strict");
const { createHash } = require("crypto");
const fs = require("fs/promises");
const os = require("os");
const path = require("path");
const test = require("node:test");
const {
  archiveName,
  checksumFor,
  install,
  platformTarget,
  verifyChecksum,
} = require("../lib/install.js");

test("maps supported runtime platforms to release archives", () => {
  assert.equal(archiveName("0.1.0", platformTarget("darwin", "arm64")), "ce_0.1.0_darwin_arm64.tar.gz");
  assert.equal(archiveName("0.1.0", platformTarget("win32", "x64")), "ce_0.1.0_windows_amd64.zip");
  assert.throws(() => platformTarget("freebsd", "x64"), /unsupported platform/);
});

test("finds and verifies release checksums", () => {
  const archive = Buffer.from("release archive");
  const checksum = createHash("sha256").update(archive).digest("hex");
  const checksums = `${checksum}  ce_0.1.0_linux_amd64.tar.gz\n`;
  assert.equal(checksumFor(checksums, "ce_0.1.0_linux_amd64.tar.gz"), checksum);
  verifyChecksum(archive, checksum, "ce_0.1.0_linux_amd64.tar.gz");
  assert.throws(() => verifyChecksum(archive, "0".repeat(64), "release.tar.gz"), /checksum mismatch/);
});

test("installs only a checksum-verified extracted binary", async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "ce-npm-wrapper-test-"));
  const archive = Buffer.from("release archive");
  const checksum = createHash("sha256").update(archive).digest("hex");
  const filename = "ce_0.1.0_linux_amd64.tar.gz";
  const fetchImpl = async (url) => ({
    ok: true,
    status: 200,
    arrayBuffer: async () =>
      (url.endsWith("checksums.txt")
        ? Buffer.from(`${checksum}  ${filename}\n`)
        : archive),
  });

  try {
    const destination = await install({
      root,
      version: "0.1.0",
      platform: "linux",
      architecture: "x64",
      baseURL: "https://releases.example.test/v0.1.0",
      fetchImpl,
      extract: async (_archive, extracted) => {
        await fs.mkdir(path.join(extracted, "ce_0.1.0_linux_amd64"));
        await fs.writeFile(path.join(extracted, "ce_0.1.0_linux_amd64", "ce"), "binary");
      },
    });
    assert.equal(await fs.readFile(destination, "utf8"), "binary");
  } finally {
    await fs.rm(root, { recursive: true, force: true });
  }
});
