import { Check, ChevronDown } from "lucide-react";
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent
} from "react";
import { FloatingMenuPortal, isInsideFloatingMenu } from "./ComposerFloatingMenu";
import type { FloatingMenuAlign, FloatingMenuPlacement } from "./ComposerTypes";
import { useI18n } from "./i18n";

// SelectMenu — the product-styled replacement for a native <select>.
//
// The native control renders its option list with the operating system's
// own popup, which never matches the app's floating-menu look (rounded,
// blurred, theme-aware — see ComposerRuntimeMenus). This component
// keeps a form-field trigger button but renders its options through the
// shared FloatingMenuPortal so every dropdown in the product speaks the
// same visual language.

export type SelectMenuOption = {
  value: string;
  label: string;
  // Optional dimmed secondary line (e.g. the provider a model belongs to).
  hint?: string;
  disabled?: boolean;
};

export type SelectMenuGroup = {
  // Omit for an ungrouped block (no header row).
  label?: string;
  options: SelectMenuOption[];
};

export function SelectMenu({
  value,
  onChange,
  options,
  groups,
  disabled = false,
  placeholder,
  ariaLabel,
  id,
  className,
  triggerClassName,
  dataTestid,
  dataField,
  placement = "below",
  align = "left",
  // When true, the floating menu flips to the opposite side of the
  // trigger if the requested placement doesn't have enough viewport
  // room (e.g. the model picker in the new-participant dialog, where
  // the list of providers × models can easily exceed the space below
  // the trigger inside a centered modal). See FloatingMenuPortal for
  // the actual flip heuristic.
  flip = false
}: {
  value: string;
  onChange: (value: string) => void;
  // Provide EITHER a flat option list or grouped options.
  options?: SelectMenuOption[];
  groups?: SelectMenuGroup[];
  disabled?: boolean;
  placeholder?: string;
  ariaLabel?: string;
  id?: string;
  className?: string;
  triggerClassName?: string;
  dataTestid?: string;
  dataField?: string;
  placement?: FloatingMenuPlacement;
  align?: FloatingMenuAlign;
  flip?: boolean;
}): JSX.Element {
  const { t } = useI18n();
  const anchorRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const [open, setOpen] = useState(false);
  const [menuWidth, setMenuWidth] = useState(220);
  const [activeIndex, setActiveIndex] = useState(-1);

  const resolvedGroups: SelectMenuGroup[] =
    groups ?? (options ? [{ options }] : []);
  const flatOptions = resolvedGroups.flatMap((group) => group.options);
  const selected = flatOptions.find((option) => option.value === value);
  const triggerLabel = selected?.label ?? placeholder ?? t("common.select");
  const hasSelection = selected !== undefined;

  function firstEnabledIndex(): number {
    return flatOptions.findIndex((option) => !option.disabled);
  }

  function nextEnabledIndex(from: number, direction: 1 | -1): number {
    if (flatOptions.length === 0) {
      return -1;
    }
    let index = from;
    for (let step = 0; step < flatOptions.length; step += 1) {
      index += direction;
      if (index < 0) {
        index = flatOptions.length - 1;
      } else if (index >= flatOptions.length) {
        index = 0;
      }
      if (!flatOptions[index]?.disabled) {
        return index;
      }
    }
    return from;
  }

  // Measure the trigger so the panel matches the field width — this is a
  // form control, not a floating chip, so a panel narrower/wider than its
  // trigger would read as a different, misaligned element.
  useLayoutEffect(() => {
    if (open && anchorRef.current) {
      setMenuWidth(Math.max(anchorRef.current.offsetWidth, 160));
    }
  }, [open]);

  // On open, seed the keyboard cursor on the current selection (or the
  // first selectable option) and move DOM focus onto it.
  useEffect(() => {
    if (!open) {
      return;
    }
    const selectedIndex = selected ? flatOptions.indexOf(selected) : -1;
    setActiveIndex(selectedIndex >= 0 ? selectedIndex : firstEnabledIndex());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  useEffect(() => {
    if (open && activeIndex >= 0) {
      itemRefs.current[activeIndex]?.focus();
    }
  }, [open, activeIndex]);

  // Self-contained dismissal — pointerdown outside the trigger/portal and
  // Escape both close. Mirrors ChatFocusChip so this control needs no
  // wiring into the host's shared floating-menu registry.
  useEffect(() => {
    if (!open) {
      return undefined;
    }
    function handlePointerDown(event: PointerEvent): void {
      const target = event.target;
      if (!(target instanceof Node)) {
        return;
      }
      if (anchorRef.current?.contains(target)) {
        return;
      }
      if (isInsideFloatingMenu(target, "select-menu")) {
        return;
      }
      setOpen(false);
    }
    document.addEventListener("pointerdown", handlePointerDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
    };
  }, [open]);

  function commit(nextValue: string): void {
    onChange(nextValue);
    setOpen(false);
    triggerRef.current?.focus();
  }

  function handleTriggerKeyDown(event: ReactKeyboardEvent<HTMLButtonElement>): void {
    if (disabled) {
      return;
    }
    if (
      event.key === "ArrowDown" ||
      event.key === "ArrowUp" ||
      event.key === "Enter" ||
      event.key === " "
    ) {
      event.preventDefault();
      setOpen(true);
    }
  }

  function handleMenuKeyDown(event: ReactKeyboardEvent<HTMLDivElement>): void {
    if (event.key === "Escape") {
      event.preventDefault();
      setOpen(false);
      triggerRef.current?.focus();
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      setActiveIndex((current) => nextEnabledIndex(current, 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActiveIndex((current) => nextEnabledIndex(current, -1));
    } else if (event.key === "Home") {
      event.preventDefault();
      setActiveIndex(firstEnabledIndex());
    } else if (event.key === "End") {
      event.preventDefault();
      setActiveIndex(nextEnabledIndex(0, -1));
    } else if (event.key === "Tab") {
      setOpen(false);
    }
  }

  // Running index across groups so itemRefs line up with flatOptions.
  let flatIndex = -1;

  return (
    <div
      className={className ? `select-menu ${className}` : "select-menu"}
      ref={anchorRef}
    >
      <button
        type="button"
        id={id}
        ref={triggerRef}
        className={
          triggerClassName
            ? `select-menu-trigger ${triggerClassName}`
            : "select-menu-trigger"
        }
        data-field={dataField}
        data-testid={dataTestid}
        data-placeholder={hasSelection ? undefined : ""}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={ariaLabel}
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
        onKeyDown={handleTriggerKeyDown}
      >
        <span className="select-menu-value">{triggerLabel}</span>
        <ChevronDown className="select-menu-chevron icon" aria-hidden="true" />
      </button>
      {open ? (
        <FloatingMenuPortal
          anchorRef={anchorRef}
          owner="select-menu"
          placement={placement}
          align={align}
          width={menuWidth}
          flip={flip}
        >
          <div
            className="select-menu-panel"
            role="menu"
            aria-label={ariaLabel}
            style={{ width: menuWidth }}
            onKeyDown={handleMenuKeyDown}
          >
            {flatOptions.length === 0 ? (
              <div className="select-menu-empty">没有可选项</div>
            ) : null}
            {resolvedGroups.map((group, groupIndex) => (
              <div className="select-menu-group" key={group.label ?? `group-${groupIndex}`}>
                {groupIndex > 0 ? <div className="select-menu-separator" /> : null}
                {group.label ? (
                  <div className="select-menu-group-label">{group.label}</div>
                ) : null}
                {group.options.map((option) => {
                  flatIndex += 1;
                  const index = flatIndex;
                  const isSelected = option.value === value;
                  return (
                    <button
                      key={option.value}
                      type="button"
                      role="menuitemradio"
                      aria-checked={isSelected}
                      aria-disabled={option.disabled || undefined}
                      data-value={option.value}
                      className="select-menu-item"
                      tabIndex={index === activeIndex ? 0 : -1}
                      ref={(node) => {
                        itemRefs.current[index] = node;
                      }}
                      disabled={option.disabled}
                      onClick={() => {
                        if (!option.disabled) {
                          commit(option.value);
                        }
                      }}
                      onMouseEnter={() => {
                        if (!option.disabled) {
                          setActiveIndex(index);
                        }
                      }}
                    >
                      <span className="select-menu-item-text">
                        <span className="select-menu-item-label">{option.label}</span>
                        {option.hint ? (
                          <span className="select-menu-item-hint">{option.hint}</span>
                        ) : null}
                      </span>
                      {isSelected ? (
                        <Check className="select-menu-check icon-lg" aria-hidden="true" />
                      ) : null}
                    </button>
                  );
                })}
              </div>
            ))}
          </div>
        </FloatingMenuPortal>
      ) : null}
    </div>
  );
}
