/**
 * Friendly display names for subagents. The pool mixes literary characters,
 * symbolic aliases, and notable scientists while keeping every chip label
 * short enough for the environment panel.
 *
 * Highest-random-weight (rendezvous) hashing keeps a stable agent id mapped to
 * the same entry across reloads and machines. Adding an entry can only move an
 * id to that new entry; it cannot reshuffle ids between existing entries.
 */

export type AgentNameCategory =
  | "water-margin"
  | "three-body"
  | "tarot"
  | "zodiac"
  | "scientist"
  | "mathematician";

export type AgentName = Readonly<{
  /** Short label shown in the subagent chip. */
  displayName: string;
  /** Stable internal namespace used by the hash key. */
  category: AgentNameCategory;
  /** Human-readable origin shown in the tooltip. */
  source: string;
  /** Optional identity behind an alias, such as a Water Margin hero's name. */
  secondaryName?: string;
}>;

function createAgentName(
  displayName: string,
  category: AgentNameCategory,
  source: string,
  secondaryName?: string,
): AgentName {
  return { displayName, category, source, secondaryName };
}

export const WATER_MARGIN_NAMES: ReadonlyArray<AgentName> = [
  createAgentName("呼保义", "water-margin", "《水浒传》", "宋江"),
  createAgentName("玉麒麟", "water-margin", "《水浒传》", "卢俊义"),
  createAgentName("智多星", "water-margin", "《水浒传》", "吴用"),
  createAgentName("入云龙", "water-margin", "《水浒传》", "公孙胜"),
  createAgentName("豹子头", "water-margin", "《水浒传》", "林冲"),
  createAgentName("小旋风", "water-margin", "《水浒传》", "柴进"),
  createAgentName("花和尚", "water-margin", "《水浒传》", "鲁智深"),
  createAgentName("行者", "water-margin", "《水浒传》", "武松"),
  createAgentName("没羽箭", "water-margin", "《水浒传》", "张清"),
  createAgentName("青面兽", "water-margin", "《水浒传》", "杨志"),
  createAgentName("神行太保", "water-margin", "《水浒传》", "戴宗"),
  createAgentName("黑旋风", "water-margin", "《水浒传》", "李逵"),
  createAgentName("浪子", "water-margin", "《水浒传》", "燕青"),
  createAgentName("一丈青", "water-margin", "《水浒传》", "扈三娘"),
  createAgentName("鼓上蚤", "water-margin", "《水浒传》", "时迁"),
  createAgentName("九纹龙", "water-margin", "《水浒传》", "史进"),
];

export const THREE_BODY_NAMES: ReadonlyArray<AgentName> = [
  createAgentName("叶文洁", "three-body", "《三体》"),
  createAgentName("汪淼", "three-body", "《三体》"),
  createAgentName("史强", "three-body", "《三体》"),
  createAgentName("罗辑", "three-body", "《三体》"),
  createAgentName("章北海", "three-body", "《三体》"),
  createAgentName("程心", "three-body", "《三体》"),
  createAgentName("云天明", "three-body", "《三体》"),
  createAgentName("维德", "three-body", "《三体》"),
  createAgentName("丁仪", "three-body", "《三体》"),
  createAgentName("常伟思", "three-body", "《三体》"),
  createAgentName("杨冬", "three-body", "《三体》"),
  createAgentName("申玉菲", "three-body", "《三体》"),
  createAgentName("庄颜", "three-body", "《三体》"),
  createAgentName("关一帆", "three-body", "《三体》"),
  createAgentName("东方延绪", "three-body", "《三体》"),
  createAgentName("褚岩", "three-body", "《三体》"),
];

export const TAROT_NAMES: ReadonlyArray<AgentName> = [
  createAgentName("愚者", "tarot", "塔罗"),
  createAgentName("魔术师", "tarot", "塔罗"),
  createAgentName("女祭司", "tarot", "塔罗"),
  createAgentName("女皇", "tarot", "塔罗"),
  createAgentName("皇帝", "tarot", "塔罗"),
  createAgentName("教皇", "tarot", "塔罗"),
  createAgentName("恋人", "tarot", "塔罗"),
  createAgentName("战车", "tarot", "塔罗"),
  createAgentName("力量", "tarot", "塔罗"),
  createAgentName("隐者", "tarot", "塔罗"),
  createAgentName("命运之轮", "tarot", "塔罗"),
  createAgentName("正义", "tarot", "塔罗"),
  createAgentName("倒吊人", "tarot", "塔罗"),
  createAgentName("死神", "tarot", "塔罗"),
  createAgentName("节制", "tarot", "塔罗"),
  createAgentName("恶魔", "tarot", "塔罗"),
  createAgentName("高塔", "tarot", "塔罗"),
  createAgentName("星星", "tarot", "塔罗"),
  createAgentName("月亮", "tarot", "塔罗"),
  createAgentName("太阳", "tarot", "塔罗"),
  createAgentName("审判", "tarot", "塔罗"),
  createAgentName("世界", "tarot", "塔罗"),
];

export const ZODIAC_NAMES: ReadonlyArray<AgentName> = [
  createAgentName("白羊", "zodiac", "星座"),
  createAgentName("金牛", "zodiac", "星座"),
  createAgentName("双子", "zodiac", "星座"),
  createAgentName("巨蟹", "zodiac", "星座"),
  createAgentName("狮子", "zodiac", "星座"),
  createAgentName("处女", "zodiac", "星座"),
  createAgentName("天秤", "zodiac", "星座"),
  createAgentName("天蝎", "zodiac", "星座"),
  createAgentName("射手", "zodiac", "星座"),
  createAgentName("摩羯", "zodiac", "星座"),
  createAgentName("水瓶", "zodiac", "星座"),
  createAgentName("双鱼", "zodiac", "星座"),
];

export const SCIENTIST_NAMES: ReadonlyArray<AgentName> = [
  createAgentName("爱因斯坦", "scientist", "科学家"),
  createAgentName("达尔文", "scientist", "科学家"),
  createAgentName("居里夫人", "scientist", "科学家"),
  createAgentName("费曼", "scientist", "科学家"),
  createAgentName("霍金", "scientist", "科学家"),
  createAgentName("特斯拉", "scientist", "科学家"),
  createAgentName("法拉第", "scientist", "科学家"),
  createAgentName("麦克斯韦", "scientist", "科学家"),
  createAgentName("玻尔", "scientist", "科学家"),
  createAgentName("薛定谔", "scientist", "科学家"),
  createAgentName("伽利略", "scientist", "科学家"),
  createAgentName("钱学森", "scientist", "科学家"),
  createAgentName("邓稼先", "scientist", "科学家"),
  createAgentName("屠呦呦", "scientist", "科学家"),
  createAgentName("袁隆平", "scientist", "科学家"),
];

export const MATHEMATICIAN_NAMES: ReadonlyArray<AgentName> = [
  createAgentName("欧几里得", "mathematician", "数学家"),
  createAgentName("阿基米德", "mathematician", "数学家"),
  createAgentName("祖冲之", "mathematician", "数学家"),
  createAgentName("华罗庚", "mathematician", "数学家"),
  createAgentName("陈景润", "mathematician", "数学家"),
  createAgentName("高斯", "mathematician", "数学家"),
  createAgentName("欧拉", "mathematician", "数学家"),
  createAgentName("黎曼", "mathematician", "数学家"),
  createAgentName("希尔伯特", "mathematician", "数学家"),
  createAgentName("图灵", "mathematician", "数学家"),
  createAgentName("冯诺依曼", "mathematician", "数学家"),
  createAgentName("拉马努金", "mathematician", "数学家"),
  createAgentName("康托", "mathematician", "数学家"),
  createAgentName("笛卡尔", "mathematician", "数学家"),
  createAgentName("帕斯卡", "mathematician", "数学家"),
];

export const AGENT_NAMES: ReadonlyArray<AgentName> = [
  ...WATER_MARGIN_NAMES,
  ...THREE_BODY_NAMES,
  ...TAROT_NAMES,
  ...ZODIAC_NAMES,
  ...SCIENTIST_NAMES,
  ...MATHEMATICIAN_NAMES,
];

const UTF8_ENCODER = new TextEncoder();

/** FNV-1a over UTF-8 bytes, kept in unsigned 32-bit arithmetic. */
export function fnv1aUTF8(value: string): number {
  let hash = 0x811c9dc5;
  for (const byte of UTF8_ENCODER.encode(value)) {
    hash ^= byte;
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash >>> 0;
}

function stableKey(entry: AgentName): string {
  return `${entry.category}:${entry.displayName}`;
}

/**
 * Select from an arbitrary non-empty pool. Exposed so append-only stability
 * and candidate-order independence remain directly testable.
 */
export function selectAgentName(agentID: string, pool: ReadonlyArray<AgentName>): AgentName {
  if (pool.length === 0) {
    throw new Error("Agent name pool must not be empty");
  }

  const safeID = agentID || "agent";
  let selected: AgentName | undefined;
  let selectedKey = "";
  let selectedScore = -1;

  for (const entry of pool) {
    const key = stableKey(entry);
    const score = fnv1aUTF8(`${safeID}\0${key}`);
    if (score > selectedScore || (score === selectedScore && key < selectedKey)) {
      selected = entry;
      selectedKey = key;
      selectedScore = score;
    }
  }

  if (!selected) {
    throw new Error("Agent name pool must not be empty");
  }
  return selected;
}

/** Resolve the stable friendly display identity for a subagent id. */
export function agentNameForSubagentID(agentID: string): AgentName {
  return selectAgentName(agentID, AGENT_NAMES);
}
