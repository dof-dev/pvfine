const fs = require("node:fs");
const path = require("node:path");

const version = process.argv[2];
if (!version || !/^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(version)) {
  throw new Error(`Invalid release version: ${version || "<empty>"}`);
}

const root = path.resolve(__dirname, "..");

function read(file) {
  return fs.readFileSync(path.join(root, file), "utf8");
}

function write(file, content) {
  fs.writeFileSync(path.join(root, file), content);
}

function replaceOnce(file, pattern, replacement) {
  const content = read(file);
  if (!pattern.test(content)) {
    throw new Error(`Version field not found in ${file}`);
  }
  write(file, content.replace(pattern, replacement));
}

const windowsInfoFile = "build/windows/info.json";
const windowsInfo = JSON.parse(read(windowsInfoFile));
windowsInfo.fixed.file_version = version;
windowsInfo.info["0000"].ProductVersion = version;
write(windowsInfoFile, `${JSON.stringify(windowsInfo, null, "\t")}\n`);

for (const file of ["build/darwin/Info.plist", "build/darwin/Info.dev.plist"]) {
  replaceOnce(
    file,
    /(<key>CFBundleVersion<\/key>\s*<string>)[^<]*(<\/string>)/,
    `$1${version}$2`,
  );
  replaceOnce(
    file,
    /(<key>CFBundleShortVersionString<\/key>\s*<string>)[^<]*(<\/string>)/,
    `$1${version}$2`,
  );
}

const configFile = "build/config.yml";
const configLines = read(configFile).split("\n");
let inInfo = false;
let configUpdated = false;
for (let i = 0; i < configLines.length; i += 1) {
  const line = configLines[i];
  if (/^info:\s*$/.test(line)) {
    inInfo = true;
    continue;
  }
  if (inInfo && /^\S/.test(line)) {
    inInfo = false;
  }
  if (inInfo && /^\s+version:\s*"/.test(line)) {
    configLines[i] = line.replace(/version:\s*"[^"]*"/, `version: "${version}"`);
    configUpdated = true;
    break;
  }
}
if (!configUpdated) {
  throw new Error(`Version field not found in ${configFile}`);
}
write(configFile, configLines.join("\n"));

replaceOnce(
  "build/linux/nfpm/nfpm.yaml",
  /^(version:\s*")[^"]*(")/m,
  `$1${version}$2`,
);

console.log(`Updated desktop build metadata to ${version}`);
