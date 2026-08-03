import { beforeEach, describe, expect, it } from "vitest";
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

describe("channel room preferences", () => {
  it("persists personal pin and archive choices", () => {
    const pinned = togglePinnedChannelRoom(emptyChannelRoomPreferences, "room-1");
    const archived = archiveChannelRoomPreference(pinned, "room-1");
    writeChannelRoomPreferences(archived);

    const restored = readChannelRoomPreferences();
    expect(channelRoomIsPinned(restored, "room-1")).toBe(false);
    expect(channelRoomIsArchived(restored, "room-1")).toBe(true);
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
