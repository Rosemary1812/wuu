/**
 * Friendly display names for subagents. Each subagent's stable `agent.id`
 * is hashed to one of the Water Margin (《水浒传》) outlaws — the 外号
 * (folk nickname) becomes the chip title, with the 真名 (real name) tucked
 * into the tooltip. The djb2 hash is deterministic: the same `agent.id`
 * maps to the same hero across reloads, sessions, and machines.
 *
 * The 108-strong pool size comes from the novel's canonical roster
 * (天罡 36 + 地煞 72). We currently ship a confident starter set of
 * the well-attested nicknames; extend the array by appending new
 * `{ nickname, realName }` entries — order matters because it is the
 * slot the hash resolves to. Uniqueness within a single session is
 * guaranteed as long as the live subagent count stays below the
 * number of distinct nicknames, which is the design's own safety
 * margin (real sessions hold a handful of subagents at once).
 */

export type SubagentNickname = {
  /** Folk 外号 shown as the chip title; always short (2-4 Chinese chars). */
  nickname: string;
  /** Character's 真名 for the tooltip's secondary line. */
  realName: string;
};

export const WATER_MARGIN_HEROES: ReadonlyArray<SubagentNickname> = [
  // 天罡 — Heavenly Spirits (confident set)
  { nickname: "呼保义", realName: "宋江" },
  { nickname: "玉麒麟", realName: "卢俊义" },
  { nickname: "智多星", realName: "吴用" },
  { nickname: "入云龙", realName: "公孙胜" },
  { nickname: "大刀", realName: "关胜" },
  { nickname: "豹子头", realName: "林冲" },
  { nickname: "霹雳火", realName: "秦明" },
  { nickname: "双鞭", realName: "呼延灼" },
  { nickname: "小旋风", realName: "柴进" },
  { nickname: "扑天雕", realName: "李应" },
  { nickname: "美髯公", realName: "朱仝" },
  { nickname: "花和尚", realName: "鲁智深" },
  { nickname: "行者", realName: "武松" },
  { nickname: "双枪将", realName: "董平" },
  { nickname: "没羽箭", realName: "张清" },
  { nickname: "青面兽", realName: "杨志" },
  { nickname: "金枪手", realName: "徐宁" },
  { nickname: "急先锋", realName: "索超" },
  { nickname: "神行太保", realName: "戴宗" },
  { nickname: "铁面孔明", realName: "宋清" },
  { nickname: "没遮拦", realName: "穆弘" },
  { nickname: "插翅虎", realName: "雷横" },
  { nickname: "黑旋风", realName: "李逵" },
  { nickname: "活阎罗", realName: "阮小七" },
  { nickname: "病尉迟", realName: "杨雄" },
  { nickname: "拼命三郎", realName: "石秀" },
  { nickname: "浪子", realName: "燕青" },

  // 地煞 — Earthly Fiends (well-attested nicknames only)
  { nickname: "一丈青", realName: "扈三娘" },
  { nickname: "鼓上蚤", realName: "时迁" },
  { nickname: "九纹龙", realName: "史进" },
];

function hashStringToIndex(agentID: string): number {
  // djb2 — fast, deterministic across runs, stable across JS engines.
  let hash = 5381;
  for (let i = 0; i < agentID.length; i++) {
    hash = ((hash << 5) + hash + agentID.charCodeAt(i)) >>> 0;
  }
  return hash % WATER_MARGIN_HEROES.length;
}

/**
 * Resolve the friendly nickname + real name for a subagent id. The same id
 * always returns the same entry, even across process restarts.
 */
export function nicknameForSubagentID(agentID: string): SubagentNickname {
  const safeID = agentID || "agent";
  const entry = WATER_MARGIN_HEROES[hashStringToIndex(safeID)];
  if (!entry) {
    return { nickname: safeID, realName: safeID };
  }
  return entry;
}
