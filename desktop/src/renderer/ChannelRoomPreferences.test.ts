import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  archiveChannelRoomPreference,
  channelRoomIsArchived,
  channelRoomIsPinned,
  emptyChannelRoomPreferences,
  readChannelRoomPreferences,
  togglePinnedChannelRoom,
  unarchiveChannelRoomPreference,
  visibleChannelRooms,
  writeChannelRoomPreferences,
} from "./ChannelRoomPreferences";

beforeEach(() => {
  window.localStorage.clear();
});

const originalWuu = window.wuu;
afterEach(() => {
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: originalWuu,
  });
});

describe("channel room preferences", () => {
  it("persists personal pin and archive choices", () => {
    const pinned = togglePinnedChannelRoom(emptyChannelRoomPreferences, "room-1");
    const archived = archiveChannelRoomPreference(pinned, "room-1");
    writeChannelRoomPreferences(archived);

    const restored = readChannelRoomPreferences();
    expect(channelRoomIsPinned(restored, "room-1")).toBe(false);
    expect(channelRoomIsArchived(restored, "room-1")).toBe(true);
  });

  it("prefers desktop persistence over origin-bound local storage", () => {
    window.localStorage.setItem(
      "wuu.channels.roomPreferences",
      JSON.stringify({ pinnedRoomIDs: [], archivedRoomIDs: ["stale-room"] }),
    );
    Object.defineProperty(window, "wuu", {
      configurable: true,
      value: {
        initialChannelRoomPreferences: {
          pinnedRoomIDs: ["room-2"],
          archivedRoomIDs: ["room-1"],
        },
      },
    });

    expect(readChannelRoomPreferences()).toEqual({
      pinnedRoomIDs: ["room-2"],
      archivedRoomIDs: ["room-1"],
    });
  });

  it("restores archived rooms without pinning them", () => {
    const archived = archiveChannelRoomPreference(emptyChannelRoomPreferences, "room-1");
    const restored = unarchiveChannelRoomPreference(archived, "room-1");

    expect(restored).toEqual(emptyChannelRoomPreferences);
  });

  it("filters only archived rooms from the visible room list", () => {
    const rooms = [
      { id: "room-1" },
      { id: "room-2" },
    ] as Parameters<typeof visibleChannelRooms>[0];
    const preferences = archiveChannelRoomPreference(emptyChannelRoomPreferences, "room-1");

    expect(visibleChannelRooms(rooms, preferences).map((room) => room.id)).toEqual(["room-2"]);
  });
});
