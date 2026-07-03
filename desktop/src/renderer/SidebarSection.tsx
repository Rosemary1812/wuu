import { ChevronRight } from "lucide-react";
import {
  createContext,
  type HTMLAttributes,
  type ReactNode,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import { SectionRowIcon } from "./ThreadSidebar";

// Must match the .thread-list-collapse transition duration in sidebar.css.
const SECTION_COLLAPSE_MS = 190;

/**
 * dnd-kit activator context shared between SortableSection (AppSidebar)
 * and SidebarSection. The default value is `null`, so callsites that
 * are NOT inside a SortableSection (notably the pinned 置顶 section,
 * which is fixed-position and never reorderable) read null and render
 * the header as a plain toggle — no drag listeners leak into pinned.
 */
export type SidebarSectionDragHandle = {
  dragHandleProps: HTMLAttributes<HTMLButtonElement>;
  isDragging: boolean;
};

export const SidebarSectionDragHandleContext = createContext<SidebarSectionDragHandle | null>(
  null,
);

export function useSidebarSectionDragHandle(): SidebarSectionDragHandle | null {
  return useContext(SidebarSectionDragHandleContext);
}

/**
 * Shared section header + collapse-body component.
 *
 * One component renders the four section types in the left sidebar:
 * 置顶 / Agents / 对话 scratch / project. They all share the same visual
 * anatomy (icon + label + chevron + collapse body) and the same 190 ms
 * expand/collapse grid animation. Wiring them through one component
 * keeps icon size, spacing, and motion unified across the panel —
 * the parallel look-alike markup that the previous restructure
 * shipped led to subtle size and spacing drift between the four.
 *
 * Differences between sections surface as props rather than as
 * duplicated markup:
 *   - iconKind + CollapsedIcon / ExpandedIcon pair (pinned uses Pin /
 *     Pin with a CSS rotate; agents uses UserRound / UsersRound; 对话
 *     uses MessageSquare / MessagesSquare; project uses Folder /
 *     FolderOpen).
 *   - Optional `loading` / `unread` indicators in the right-hand grid
 *     track of the header (only project rows surface these).
 *   - Optional `actions` slot, rendered as a sibling of the header
 *     button so they stay OUTSIDE the <button>. The Agents section
 *     uses this to host its ＋ (new agent) and … (overflow) controls
 *     while keeping the toggle button a leaf DOM node (c4a3a50d landed
 *     this fix).
 *   - Optional `newItemButton` slot, rendered as a sibling `<button>`
 *     for the project-only "+ new conversation" hover affordance.
 *   - Optional `ariaLabel` / `title` overrides so the section can
 *     describe its toggle verb ("展开 / 收起 对话") and surface unread
 *     state in the label.
 *   - `emptyNote`: shown in the body when no `children` are mounted
 *     so the height collapse animation has real content.
 *
 * The close animation is self-contained: the body stays mounted with the
 * `.thread-list-collapse.closing` class for 190 ms after `expanded` flips
 * false, so all four sections share the identical collapse motion.
 */
export function SidebarSection({
  expanded,
  iconKind,
  CollapsedIcon,
  ExpandedIcon,
  label,
  ariaLabel,
  title,
  active,
  pending,
  unread,
  loading,
  actions,
  newItemButton,
  emptyNote,
  children,
  onToggle,
}: {
  expanded: boolean;
  iconKind: string;
  CollapsedIcon: React.ComponentType<{ className?: string }>;
  ExpandedIcon: React.ComponentType<{ className?: string }>;
  label: ReactNode;
  ariaLabel: string;
  title: string;
  active?: boolean;
  pending?: boolean;
  unread?: boolean;
  loading?: boolean;
  // Rendered as a sibling of the header <button> so it can stay outside
  // the toggle's interactive area (React 18 rejects nested <button>s
  // and the click semantics get muddied if the actions live inside).
  actions?: ReactNode;
  // Project rows show a + new-conversation button on hover. Rendered as
  // a sibling so it can position over the right edge of the header.
  newItemButton?: ReactNode;
  // Optional placeholder row shown when the body has no children. Stays
  // mounted while expanded so the height-collapse animation has content
  // to animate (a 0→0 grid transition would otherwise vanish).
  emptyNote?: ReactNode;
  onToggle: () => void;
  children?: ReactNode;
}): JSX.Element {
  // Pulled from the SortableSection provider so the header <button>
  // becomes the dnd-kit activator and the reduced-opacity visual lands
  // on the dragged source. Null when this section isn't sortable (置顶
  // — fixed-position and never inside a SortableContext).
  const dragHandle = useSidebarSectionDragHandle();
  const dragHandleProps = dragHandle?.dragHandleProps;
  const isDragging = dragHandle?.isDragging ?? false;
  // Self-contained close animation. When `expanded` flips true → false the
  // body stays mounted for one SECTION_COLLAPSE_MS window with the
  // `.closing` class so the grid-rows height transition can play; only
  // after the window does the body unmount. Re-expanding mid-close cancels
  // the timer and the still-mounted body animates back open. This gives
  // every section (置顶 / Agents / 对话 / 项目) the same close motion
  // without each parent having to orchestrate a collapsing phase.
  const [closing, setClosing] = useState(false);
  const prevExpandedRef = useRef(expanded);
  useEffect(() => {
    const wasExpanded = prevExpandedRef.current;
    prevExpandedRef.current = expanded;
    if (expanded) {
      setClosing(false);
      return;
    }
    if (!wasExpanded) {
      return;
    }
    setClosing(true);
    const timer = window.setTimeout(() => {
      setClosing(false);
    }, SECTION_COLLAPSE_MS);
    return () => {
      window.clearTimeout(timer);
    };
  }, [expanded]);
  const headerClassName = [
    "project-row",
    "sidebar-section-row",
    active ? "active" : "",
    expanded ? "expanded" : "",
    pending ? "pending-switch" : "",
    unread ? "has-unread" : "",
    isDragging ? "dragging" : "",
    dragHandleProps ? "can-reorder" : "",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <>
      <div className="sidebar-section-header-group">
        <button
          className={headerClassName}
          type="button"
          aria-expanded={expanded}
          aria-label={ariaLabel}
          aria-busy={pending || undefined}
          aria-current={active ? "page" : undefined}
          title={title}
          onClick={onToggle}
          {...dragHandleProps}
        >
          <SectionRowIcon
            collapsed={!expanded}
            iconKind={iconKind}
            CollapsedIcon={CollapsedIcon}
            ExpandedIcon={ExpandedIcon}
          />
          <span className="project-row-label">
            <span className="project-row-name">{label}</span>
            <ChevronRight
              className="project-row-chevron icon"
              aria-hidden="true"
            />
          </span>
          {loading ? (
            <span className="project-row-loading" aria-hidden="true" />
          ) : null}
          {unread && !loading ? (
            <span className="project-row-unread" aria-hidden="true" />
          ) : null}
        </button>
        {newItemButton}
        {actions}
      </div>
      {(expanded || closing) && (children || emptyNote) ? (
        <div
          className={`thread-list-collapse${closing ? " closing" : ""}`}
          aria-hidden={closing || undefined}
        >
          {children ?? (
            <div className="sidebar-section-empty-note">{emptyNote}</div>
          )}
        </div>
      ) : null}
    </>
  );
}
