// Connection-state strip under the header. Quiet by design: nothing renders
// while attached; connecting/reconnecting shows a hairline strip with the
// eyes sticker (the one sanctioned decoration for this state).

import { Image, StyleSheet, Text, View } from "react-native";

import type { ConnectionPhase } from "../lib/store";
import { usePalette } from "../theme";

import EYES from "../../assets/reactions/eyes.png";

export function ConnectionBanner({ phase }: { phase: ConnectionPhase }): React.JSX.Element | null {
  const palette = usePalette();
  if (phase === "attached" || phase === "idle") return null;
  const label = phase === "connecting" ? "连接中…" : "重连中…";
  return (
    <View style={[styles.strip, { backgroundColor: palette.overlay4, borderColor: palette.hairline }]}>
      <Image source={EYES} style={styles.sticker} resizeMode="contain" />
      <Text style={[styles.label, { color: palette.inkMuted }]}>{label}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  strip: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
    paddingVertical: 5,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  sticker: { width: 18, height: 18 },
  label: { fontSize: 12 },
});
