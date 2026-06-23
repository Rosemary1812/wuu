import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  ImagePreviewProvider,
  useImagePreview,
  type ImagePreviewContextValue
} from "./ImagePreview";

let container: HTMLDivElement;
let root: Root | null = null;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
});

function overlayImage(): HTMLImageElement | null {
  return container.querySelector(".image-preview-image");
}

function overlayRoot(): HTMLElement | null {
  return container.querySelector(".image-preview-overlay");
}

function renderWithProbe(): { getAPI: () => ImagePreviewContextValue | null } {
  const ref: { current: ImagePreviewContextValue | null } = { current: null };

  function Probe(): null {
    ref.current = useImagePreview();
    return null;
  }

  act(() => {
    root = createRoot(container);
    root.render(
      <ImagePreviewProvider>
        <Probe />
      </ImagePreviewProvider>
    );
  });

  return {
    getAPI: () => ref.current
  };
}

describe("ImagePreviewProvider", () => {
  it("does not render the overlay when nothing is open", () => {
    renderWithProbe();
    expect(overlayRoot()).toBeNull();
  });

  it("renders the image when openPreview is called and hides it when closePreview is called", () => {
    const probe = renderWithProbe();
    act(() => {
      probe.getAPI()?.openPreview({
        src: "data:image/png;base64,iVBORw0KGgo=",
        alt: "Sample",
        title: "Sample title"
      });
    });
    const previewImage = overlayImage();
    expect(previewImage).not.toBeNull();
    expect(previewImage?.getAttribute("src")).toContain("data:image/png");
    expect(previewImage?.getAttribute("alt")).toBe("Sample");

    act(() => {
      probe.getAPI()?.closePreview();
    });
    expect(overlayRoot()).toBeNull();
  });

  it("resets the visible image when openPreview receives a new source", () => {
    const probe = renderWithProbe();
    act(() => {
      probe.getAPI()?.openPreview({ src: "data:image/png;base64,AAA" });
    });
    expect(overlayImage()?.getAttribute("src")).toContain("AAA");
    act(() => {
      probe.getAPI()?.openPreview({ src: "data:image/png;base64,BBB" });
    });
    expect(overlayImage()?.getAttribute("src")).toContain("BBB");
  });
});

describe("useImagePreview", () => {
  it("throws when used outside an ImagePreviewProvider", () => {
    function Naked(): null {
      useImagePreview();
      return null;
    }
    expect(() => {
      act(() => {
        root = createRoot(container);
        root.render(<Naked />);
      });
    }).toThrow(/ImagePreviewProvider/);
  });
});
