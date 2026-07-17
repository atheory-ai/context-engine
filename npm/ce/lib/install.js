"use strict";

const { createHash } = require("crypto");
const { execFile } = require("child_process");
const { promisify } = require("util");
const fs = require("fs/promises");
const fsSync = require("fs");
const os = require("os");
const path = require("path");

const execFileAsync = promisify(execFile);
const OWNER = "atheory-ai";
const REPOSITORY = "context-engine";
const BINARY_NAME = process.platform === "win32" ? "ce.exe" : "ce";
const packageRoot = path.resolve(__dirname, "..");

function platformTarget(platform = process.platform, architecture = process.arch) {
  const targets = {
    "darwin-arm64": { os: "darwin", arch: "arm64", format: "tar.gz" },
    "darwin-x64": { os: "darwin", arch: "amd64", format: "tar.gz" },
    "linux-arm64": { os: "linux", arch: "arm64", format: "tar.gz" },
    "linux-x64": { os: "linux", arch: "amd64", format: "tar.gz" },
    "win32-arm64": { os: "windows", arch: "arm64", format: "zip" },
    "win32-x64": { os: "windows", arch: "amd64", format: "zip" },
  };
  const target = targets[`${platform}-${architecture}`];
  if (!target) {
    throw new Error(
      `ce: unsupported platform "${platform}-${architecture}"\n` +
        `Supported platforms: ${Object.keys(targets).join(", ")}`,
    );
  }
  return target;
}

function archiveName(version, target) {
  return `ce_${version}_${target.os}_${target.arch}.${target.format}`;
}

function releaseBaseURL(version) {
  return (
    process.env.CE_RELEASE_BASE_URL ||
    `https://github.com/${OWNER}/${REPOSITORY}/releases/download/v${version}`
  ).replace(/\/$/, "");
}

function checksumFor(checksums, filename) {
  const match = checksums
    .split(/\r?\n/)
    .map((line) => line.trim().match(/^([a-fA-F0-9]{64})\s+\*?(.+)$/))
    .find((entry) => entry && entry[2] === filename);
  if (!match) throw new Error(`ce: checksum for ${filename} was not found`);
  return match[1].toLowerCase();
}

function verifyChecksum(content, expected, filename) {
  const actual = createHash("sha256").update(content).digest("hex");
  if (actual !== expected) {
    throw new Error(`ce: checksum mismatch for ${filename}`);
  }
}

async function download(url, fetchImpl = globalThis.fetch) {
  const response = await fetchImpl(url);
  if (!response.ok) {
    throw new Error(`ce: download failed (${response.status}) for ${url}`);
  }
  return Buffer.from(await response.arrayBuffer());
}

async function extractArchive(archive, destination, target) {
  if (target.format === "zip") {
    await execFileAsync("powershell.exe", [
      "-NoProfile",
      "-NonInteractive",
      "-Command",
      "Expand-Archive -LiteralPath $args[0] -DestinationPath $args[1] -Force",
      archive,
      destination,
    ]);
    return;
  }
  await execFileAsync("tar", ["-xzf", archive, "-C", destination]);
}

async function findBinary(directory, name) {
  const entries = await fs.readdir(directory, { withFileTypes: true });
  for (const entry of entries) {
    const candidate = path.join(directory, entry.name);
    if (entry.isFile() && entry.name === name) return candidate;
    if (entry.isDirectory()) {
      const found = await findBinary(candidate, name);
      if (found) return found;
    }
  }
  return undefined;
}

function binaryPath(root = packageRoot, platform = process.platform) {
  const binary = path.join(root, "bin", platform === "win32" ? "ce.exe" : "ce");
  if (!fsSync.existsSync(binary)) {
    throw new Error(
      "ce: the release binary is missing. Reinstall @atheory-ai/ce without --ignore-scripts, " +
        "or set CE_SKIP_DOWNLOAD=1 only when you do not intend to run ce.",
    );
  }
  return binary;
}

async function install({
  root = packageRoot,
  version = require("../package.json").version,
  platform = process.platform,
  architecture = process.arch,
  baseURL = releaseBaseURL(version),
  fetchImpl = globalThis.fetch,
  extract = extractArchive,
} = {}) {
  const target = platformTarget(platform, architecture);
  const filename = archiveName(version, target);
  const destination = path.join(root, "bin", platform === "win32" ? "ce.exe" : "ce");
  const tmp = await fs.mkdtemp(path.join(os.tmpdir(), "ce-npm-"));

  try {
    const [checksums, archive] = await Promise.all([
      download(`${baseURL}/checksums.txt`, fetchImpl),
      download(`${baseURL}/${filename}`, fetchImpl),
    ]);
    verifyChecksum(archive, checksumFor(checksums.toString("utf8"), filename), filename);

    const archivePath = path.join(tmp, filename);
    const extracted = path.join(tmp, "extract");
    await fs.mkdir(extracted);
    await fs.writeFile(archivePath, archive);
    await extract(archivePath, extracted, target);

    const source = await findBinary(extracted, platform === "win32" ? "ce.exe" : "ce");
    if (!source) throw new Error(`ce: ${BINARY_NAME} was not found in ${filename}`);

    await fs.mkdir(path.dirname(destination), { recursive: true });
    await fs.copyFile(source, destination);
    if (platform !== "win32") await fs.chmod(destination, 0o755);
    return destination;
  } finally {
    await fs.rm(tmp, { recursive: true, force: true });
  }
}

async function main() {
  if (process.env.CE_SKIP_DOWNLOAD === "1") return;
  const destination = await install();
  process.stdout.write(`Installed Context Engine binary to ${destination}\n`);
}

if (require.main === module) {
  main().catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exit(1);
  });
}

module.exports = {
  archiveName,
  binaryPath,
  checksumFor,
  install,
  platformTarget,
  verifyChecksum,
};
