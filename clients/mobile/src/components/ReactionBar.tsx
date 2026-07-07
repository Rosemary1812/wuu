// Message reactions: the same six-sticker vocabulary as the desktop
// (lockstep with the backend), rendered from the identical PNG art so the
// two ends read as one product. Aggregated chips under a bubble; a picker
// sheet opens on long-press.

import {
  Image,
  ImageSourcePropType,
  Modal,
  Pressable,
  StyleSheet,
  Text,
  View,
} from "react-native";
import type { MessageMarkWire } from "@wuu/protocol";

import { usePalette } from "../theme";

import eyesArt from "../../assets/reactions/eyes.png";
import niceArt from "../../assets/reactions/nice.png";
import shrugArt from "../../assets/reactions/shrug.png";
import sideeyeArt from "../../assets/reactions/sideeye.png";
import smugArt from "../../assets/reactions/smug.png";
import whoaArt from "../../assets/reactions/whoa.png";

export const REACTION_ART: Record<string, ImageSourcePropType> = {
  eyes: eyesArt,
  nice: niceArt,
  shrug: shrugArt,
  sideeye: sideeyeArt,
  smug: smugArt,
  whoa: whoaArt,
};

export const REACTION_LABELS: Record<string, string> = {
  eyes: "看到了",
  nice: "赞同",
  shrug: "无所谓",
  sideeye: "存疑",
  smug: "得意",
  whoa: "惊讶",
};

export const REACTION_KEYS = ["eyes", "nice", "shrug", "sideeye", "smug", "whoa"] as const;

/** Aggregated reaction chips for one message (marks already filtered to its
 *  seq). */
export function ReactionChips({ marks }: { marks: MessageMarkWire[] }): React.JSX.Element | null {
  const palette = usePalette();
  const byReaction = new Map<string, number>();
  for (const mark of marks) {
    if (mark.kind !== "reaction" || !mark.reaction) continue;
    byReaction.set(mark.reaction, (byReaction.get(mark.reaction) ?? 0) + 1);
  }
  if (byReaction.size === 0) return null;
  return (
    <View style={styles.chipRow}>
      {[...byReaction.entries()].map(([key, count]) => (
        <View
          key={key}
          style={[styles.chip, { backgroundColor: palette.overlay4, borderColor: palette.hairline }]}
        >
          {REACTION_ART[key] ? (
            <Image source={REACTION_ART[key]} style={styles.chipArt} resizeMode="contain" />
          ) : (
            <Text style={styles.chipFallback}>{key}</Text>
          )}
          {count > 1 ? (
            <Text style={[styles.chipCount, { color: palette.inkMuted }]}>{count}</Text>
          ) : null}
        </View>
      ))}
    </View>
  );
}

/** WeChat-style picker: big sticker + label grid in a bottom sheet. */
export function ReactionPicker({
  visible,
  onPick,
  onClose,
}: {
  visible: boolean;
  onPick: (key: string) => void;
  onClose: () => void;
}): React.JSX.Element {
  const palette = usePalette();
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      <Pressable style={styles.backdrop} onPress={onClose}>
        <View style={[styles.sheet, { backgroundColor: palette.paper, borderColor: palette.hairline }]}>
          <View style={styles.grid}>
            {REACTION_KEYS.map((key) => (
              <Pressable
                key={key}
                style={styles.option}
                onPress={() => {
                  onPick(key);
                  onClose();
                }}
              >
                <Image source={REACTION_ART[key]} style={styles.optionArt} resizeMode="contain" />
                <Text style={[styles.optionLabel, { color: palette.inkSoft }]}>
                  {REACTION_LABELS[key]}
                </Text>
              </Pressable>
            ))}
          </View>
        </View>
      </Pressable>
    </Modal>
  );
}

const styles = StyleSheet.create({
  chipRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 4,
    marginTop: 4,
  },
  chip: {
    flexDirection: "row",
    alignItems: "center",
    gap: 3,
    paddingHorizontal: 6,
    paddingVertical: 1,
    borderRadius: 999,
    borderWidth: StyleSheet.hairlineWidth,
  },
  chipArt: { width: 18, height: 18 },
  chipFallback: { fontSize: 12 },
  chipCount: { fontSize: 11, fontVariant: ["tabular-nums"] },
  backdrop: {
    flex: 1,
    backgroundColor: "rgba(0,0,0,0.25)",
    justifyContent: "flex-end",
  },
  sheet: {
    borderTopLeftRadius: 16,
    borderTopRightRadius: 16,
    borderWidth: StyleSheet.hairlineWidth,
    paddingVertical: 20,
    paddingHorizontal: 16,
    paddingBottom: 34,
  },
  grid: {
    flexDirection: "row",
    flexWrap: "wrap",
    justifyContent: "space-around",
    rowGap: 18,
  },
  option: { alignItems: "center", gap: 6, width: 72 },
  optionArt: { width: 40, height: 40 },
  optionLabel: { fontSize: 11 },
});
