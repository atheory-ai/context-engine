#!/usr/bin/env node

import { chmod, copyFile, mkdir, readdir, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { join } from "node:path";

const mappings = [
  { os: "darwin", arch: "arm64", packageDir: "darwin-arm64", binary: "ce" },
  { os: "darwin", arch: "amd64", packageDir: "darwin-x64", binary: "ce" },
  { os: "linux", arch: "arm64", packageDir: "linux-arm64", binary: "ce" },
  { os: "linux", arch: "amd64", packageDir: "linux-x64", binary: "ce" },
  { os: "windows", arch: "arm64", packageDir: "win32-arm64", binary: "ce.exe" },
  { os: "windows", arch: "amd64", packageDir: "win32-x64", binary: "ce.exe" },
];

const dist = "dist";

for (const mapping of mappings) {
  const source = await findBuiltBinary(mapping.os, mapping.arch, mapping.binary);
  const binDir = join("npm", mapping.packageDir, "bin");
  const target = join(binDir, mapping.binary);

  await rm(binDir, { recursive: true, force: true });
  await mkdir(binDir, { recursive: true });
  await copyFile(source, target);

  if (!target.endsWith(".exe")) {
    await chmod(target, 0o755);
  }
}

async function findBuiltBinary(os, arch, binary) {
  if (!existsSync(dist)) {
    throw new Error("dist/ does not exist; run make build-cross first");
  }

  const entries = await readdir(dist, { withFileTypes: true });
  const candidates = [];

  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    const name = entry.name;
    if (!name.includes(os) || !name.includes(arch)) continue;

    const path = join(dist, name, binary);
    if (existsSync(path)) candidates.push(path);
  }

  if (candidates.length === 0) {
    throw new Error(`could not find ${os}/${arch} binary named ${binary} under dist/`);
  }

  candidates.sort();
  return candidates[0];
}
