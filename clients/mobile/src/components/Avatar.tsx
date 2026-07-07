// Participant avatar: an uploaded avatar_image data URL when present,
// otherwise the deterministic mascot fallback (same FNV-1a assignment as
// the desktop, so identities match across ends). Metro needs static
// require() calls, hence the table here rather than in lib/avatar.ts.

import { Image, StyleSheet, View, useColorScheme } from "react-native";

import { avatarMascotIndex, avatarSeed, avatarTintIndex } from "../lib/avatar";
import { avatarTintsDark, avatarTintsLight, usePalette } from "../theme";

import mascot0 from "../../assets/avatars/mascot-0.png";
import mascot1 from "../../assets/avatars/mascot-1.png";
import mascot2 from "../../assets/avatars/mascot-2.png";
import mascot3 from "../../assets/avatars/mascot-3.png";
import mascot4 from "../../assets/avatars/mascot-4.png";
import mascot5 from "../../assets/avatars/mascot-5.png";
import mascot6 from "../../assets/avatars/mascot-6.png";
import mascot7 from "../../assets/avatars/mascot-7.png";
import mascot8 from "../../assets/avatars/mascot-8.png";
import mascot9 from "../../assets/avatars/mascot-9.png";
import mascot10 from "../../assets/avatars/mascot-10.png";
import mascot11 from "../../assets/avatars/mascot-11.png";

const MASCOTS = [
  mascot0,
  mascot1,
  mascot2,
  mascot3,
  mascot4,
  mascot5,
  mascot6,
  mascot7,
  mascot8,
  mascot9,
  mascot10,
  mascot11,
] as const;

export type AvatarStatus = "online" | "busy" | null;

export function Avatar({
  id,
  name,
  kind,
  imageUri,
  size,
  status = null,
}: {
  id?: string;
  name?: string;
  kind?: string;
  imageUri?: string;
  size: number;
  status?: AvatarStatus;
}): React.JSX.Element {
  const palette = usePalette();
  const dark = useColorScheme() === "dark";
  const seed = avatarSeed(id, name);
  const tints = dark ? avatarTintsDark : avatarTintsLight;
  const tint = tints[avatarTintIndex(seed)];
  const mascot = MASCOTS[avatarMascotIndex(seed, kind)];

  return (
    <View style={{ width: size, height: size }}>
      <View
        style={[
          styles.circle,
          { width: size, height: size, borderRadius: size / 2, backgroundColor: tint },
        ]}
      >
        {imageUri ? (
          <Image source={{ uri: imageUri }} style={styles.fill} resizeMode="cover" />
        ) : (
          // 5% padding mirrors the desktop: keeps hat tips and feet inside
          // the circular clip.
          <Image
            source={mascot}
            style={[styles.fill, { margin: size * 0.05 }]}
            resizeMode="contain"
          />
        )}
      </View>
      {status ? (
        <View
          style={[
            styles.statusDot,
            {
              backgroundColor: status === "online" ? palette.success : palette.warning,
              borderColor: palette.paper,
            },
          ]}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  circle: {
    overflow: "hidden",
    alignItems: "stretch",
  },
  fill: {
    flex: 1,
    width: undefined,
    height: undefined,
  },
  statusDot: {
    position: "absolute",
    top: -1,
    right: -1,
    width: 9,
    height: 9,
    borderRadius: 5,
    borderWidth: 1.5,
  },
});
