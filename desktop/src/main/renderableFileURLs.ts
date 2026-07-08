import { statSync } from "node:fs";
import { extname } from "node:path";

const RENDERABLE_IMAGE_EXTENSIONS = new Set([
  ".apng",
  ".avif",
  ".gif",
  ".jpeg",
  ".jpg",
  ".png",
  ".svg",
  ".webp",
]);

export function renderableFileURL(filePath: string): string {
  return `wuu-file://local/${Buffer.from(filePath, "utf8").toString("base64url")}`;
}

export function filePathFromRenderableURL(rawURL: string): string | undefined {
  try {
    const url = new URL(rawURL);
    if (url.hostname !== "local") {
      return undefined;
    }
    const encodedPath = url.pathname.replace(/^\/+/, "");
    if (!encodedPath) {
      return undefined;
    }
    return Buffer.from(encodedPath, "base64url").toString("utf8");
  } catch {
    return undefined;
  }
}

export function isRenderableImageFile(filePath: string): boolean {
  try {
    return (
      statSync(filePath).isFile() &&
      RENDERABLE_IMAGE_EXTENSIONS.has(extname(filePath).toLowerCase())
    );
  } catch {
    return false;
  }
}
