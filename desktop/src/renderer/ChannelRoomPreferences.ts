import type { ChannelRoom } from "../shared/protocol";

const CHANNEL_ROOM_PREFERENCES_KEY = "wuu.channels.roomPreferences";

export type ChannelRoomPreferences = {
  pinnedRoomIDs: string[];
  archivedRoomIDs: string[];
};

export const emptyChannelRoomPreferences: ChannelRoomPreferences = {
  pinnedRoomIDs: [],
  archivedRoomIDs: [],
};

export function readChannelRoomPreferences(): ChannelRoomPreferences {
  try {
    const raw = window.localStorage.getItem(CHANNEL_ROOM_PREFERENCES_KEY);
    if (!raw) {
      return emptyChannelRoomPreferences;
    }
    const parsed = JSON.parse(raw) as Partial<ChannelRoomPreferences>;
    const archivedRoomIDs = normalizedRoomIDs(parsed.archivedRoomIDs);
    const archived = new Set(archivedRoomIDs);
    return {
      pinnedRoomIDs: normalizedRoomIDs(parsed.pinnedRoomIDs).filter((id) => !archived.has(id)),
      archivedRoomIDs,
    };
  } catch {
    return emptyChannelRoomPreferences;
  }
}

export function writeChannelRoomPreferences(preferences: ChannelRoomPreferences): void {
  try {
    window.localStorage.setItem(CHANNEL_ROOM_PREFERENCES_KEY, JSON.stringify(preferences));
  } catch {
    // A denied/quota-limited storage write should not break the room action;
    // the in-memory preference still applies for the current window.
  }
}

export function channelRoomIsPinned(
  preferences: ChannelRoomPreferences,
  roomID: string,
): boolean {
  return preferences.pinnedRoomIDs.includes(roomID);
}

export function channelRoomIsArchived(
  preferences: ChannelRoomPreferences,
  roomID: string,
): boolean {
  return preferences.archivedRoomIDs.includes(roomID);
}

export function togglePinnedChannelRoom(
  preferences: ChannelRoomPreferences,
  roomID: string,
): ChannelRoomPreferences {
  const pinned = new Set(preferences.pinnedRoomIDs);
  if (pinned.has(roomID)) {
    pinned.delete(roomID);
  } else {
    pinned.add(roomID);
  }
  return { ...preferences, pinnedRoomIDs: Array.from(pinned) };
}

export function archiveChannelRoomPreference(
  preferences: ChannelRoomPreferences,
  roomID: string,
): ChannelRoomPreferences {
  return {
    pinnedRoomIDs: preferences.pinnedRoomIDs.filter((id) => id !== roomID),
    archivedRoomIDs: Array.from(new Set([...preferences.archivedRoomIDs, roomID])),
  };
}

export function unarchiveChannelRoomPreference(
  preferences: ChannelRoomPreferences,
  roomID: string,
): ChannelRoomPreferences {
  return {
    ...preferences,
    archivedRoomIDs: preferences.archivedRoomIDs.filter((id) => id !== roomID),
  };
}

export function visibleChannelRooms(
  rooms: ChannelRoom[],
  preferences: ChannelRoomPreferences,
): ChannelRoom[] {
  const archived = new Set(preferences.archivedRoomIDs);
  return rooms.filter((room) => !archived.has(room.id));
}

function normalizedRoomIDs(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return Array.from(
    new Set(value.filter((id): id is string => typeof id === "string" && id.trim() !== "")),
  );
}

