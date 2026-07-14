import { randomUUID } from "node:crypto";
import fs from "node:fs";
import { basename, dirname, join } from "node:path";

type AtomicWriteOptions = {
  defaultMode?: number;
};

export function writeTextFileAtomicSync(
  path: string,
  text: string,
  options: AtomicWriteOptions = {},
): void {
  const directory = dirname(path);
  fs.mkdirSync(directory, { recursive: true });

  let mode = options.defaultMode ?? 0o600;
  try {
    mode = fs.statSync(path).mode & 0o777;
  } catch (error) {
    if (!isMissingFileError(error)) {
      throw error;
    }
  }

  const temporaryPath = join(
    directory,
    `.${basename(path)}.${process.pid}.${randomUUID()}.tmp`,
  );
  let descriptor: number | undefined;
  try {
    descriptor = fs.openSync(
      temporaryPath,
      fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_WRONLY,
      mode,
    );
    fs.fchmodSync(descriptor, mode);
    fs.writeFileSync(descriptor, text, "utf8");
    fs.fsyncSync(descriptor);
    fs.closeSync(descriptor);
    descriptor = undefined;
    fs.renameSync(temporaryPath, path);
  } catch (error) {
    if (descriptor !== undefined) {
      try {
        fs.closeSync(descriptor);
      } catch {
        // Preserve the write failure below.
      }
    }
    try {
      fs.rmSync(temporaryPath, { force: true });
    } catch {
      // Preserve the write failure below.
    }
    throw error;
  }
}

function isMissingFileError(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    error.code === "ENOENT"
  );
}
