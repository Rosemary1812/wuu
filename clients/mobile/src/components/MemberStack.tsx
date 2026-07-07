// Group-member avatar stack (mirrors the desktop GroupMembersCapsule): up
// to three 20px avatars overlapping by 7px, then a +N pill.

import { StyleSheet, Text, View } from "react-native";
import type { ParticipantSummary } from "@wuu/protocol";

import { usePalette } from "../theme";
import { Avatar } from "./Avatar";

export function MemberStack({
  members,
  size = 20,
}: {
  members: ParticipantSummary[];
  size?: number;
}): React.JSX.Element | null {
  const palette = usePalette();
  if (members.length === 0) return null;
  const shown = members.slice(0, 3);
  const extra = members.length - shown.length;
  return (
    <View style={styles.row}>
      {shown.map((member, index) => (
        <View
          key={member.id}
          style={[
            index > 0 ? { marginLeft: -7 } : null,
            { borderRadius: (size + 3) / 2, borderWidth: 1.5, borderColor: palette.paper },
          ]}
        >
          <Avatar
            id={member.id}
            name={member.name}
            kind={member.kind}
            imageUri={member.avatar_image}
            size={size}
          />
        </View>
      ))}
      {extra > 0 ? (
        <View style={[styles.extra, { backgroundColor: palette.overlay6 }]}>
          <Text style={[styles.extraText, { color: palette.inkMuted }]}>+{extra}</Text>
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  row: { flexDirection: "row", alignItems: "center" },
  extra: {
    marginLeft: 3,
    paddingHorizontal: 4,
    paddingVertical: 1,
    borderRadius: 999,
  },
  extraText: { fontSize: 9, fontWeight: "600" },
});
