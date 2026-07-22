import {execFileSync} from "node:child_process";
import {readFileSync, readdirSync} from "node:fs";
import {basename, join} from "node:path";

const csv = (value) => `"${String(value ?? "").replaceAll('"', '""')}"`;
const rows = [["ecosystem", "name", "version", "license", "evidence"]];

function classify(text) {
  const normalized = text.toLowerCase();
  if (normalized.includes("apache license") && normalized.includes("version 2.0")) return "Apache-2.0";
  if (normalized.includes("mozilla public license") && normalized.includes("2.0")) return "MPL-2.0";
  if (normalized.includes("isc license")) return "ISC";
  if (normalized.includes("mit license")) return "MIT";
  if (normalized.includes("permission is hereby granted, free of charge")) return "MIT";
  if (normalized.includes("redistribution and use in source and binary forms")) {
    return normalized.includes("neither the name") ? "BSD-3-Clause" : "BSD-2-Clause";
  }
  if (normalized.includes("unicode license")) return "Unicode-3.0";
  if (normalized.includes("creative commons zero")) return "CC0-1.0";
  return "LicenseRef-See-File";
}

const moduleOutput = execFileSync("go", ["list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}{{end}}", "./cmd/esgw"], {
  encoding: "utf8",
});
for (const line of [...new Set(moduleOutput.trim().split("\n").filter(Boolean))]) {
  const [name, version, directory] = line.split("|");
  const licenseFile = readdirSync(directory).sort().find((entry) => /^(license|copying|notice)(\.|$)/i.test(entry));
  if (!licenseFile) throw new Error(`Go module ${name}@${version} has no top-level license file`);
  const contents = readFileSync(join(directory, licenseFile), "utf8");
  rows.push(["go", name, version, classify(contents), `${name}@${version}/${basename(licenseFile)}`]);
}

const lock = JSON.parse(readFileSync(new URL("../web/package-lock.json", import.meta.url), "utf8"));
for (const [path, metadata] of Object.entries(lock.packages ?? {})) {
  if (!path || !metadata.version) continue;
  const name = path.replace(/^.*node_modules\//, "");
  if (!metadata.license) throw new Error(`npm package ${name}@${metadata.version} has no license metadata`);
  rows.push(["npm", name, metadata.version, metadata.license, metadata.resolved ?? "package-lock.json"]);
}

const header = rows.shift();
rows.sort((a, b) => `${a[0]}:${a[1]}`.localeCompare(`${b[0]}:${b[1]}`));
rows.unshift(header);
process.stdout.write(`${rows.map((row) => row.map(csv).join(",")).join("\n")}\n`);
