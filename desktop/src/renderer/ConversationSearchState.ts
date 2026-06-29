import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import type {
  RuntimeContext,
  ThreadSearchResultItem,
} from "../shared/protocol";
import {
  mergeListedThreads,
  sameRuntimeContext,
  type AppState,
} from "./AppState";

const CONVERSATION_SEARCH_EXIT_MS = 180;
const CONVERSATION_SEARCH_RESULT_LIMIT = 40;

export type CloseConversationSearchOptions = {
  immediate?: boolean;
};

export type ConversationSearchState = {
  open: boolean;
  closing: boolean;
  query: string;
  loading: boolean;
  error: string;
  results: ThreadSearchResultItem[];
  selectedIndex: number;
};

const initialConversationSearch: ConversationSearchState = {
  open: false,
  closing: false,
  query: "",
  loading: false,
  error: "",
  results: [],
  selectedIndex: 0,
};

export function useConversationSearch({
  activeContext,
  getAppState,
  setAppState,
  onOpen,
  onSelectThread,
}: {
  activeContext?: RuntimeContext;
  getAppState: () => AppState;
  setAppState: (update: (current: AppState) => AppState) => void;
  onOpen: () => void;
  onSelectThread: (threadID: string) => void;
}): {
  conversationSearch: ConversationSearchState;
  conversationSearchResults: ThreadSearchResultItem[];
  conversationSearchRef: React.RefObject<HTMLDivElement | null>;
  conversationSearchInputRef: React.RefObject<HTMLInputElement | null>;
  toggleConversationSearch: () => void;
  closeConversationSearch: (options?: CloseConversationSearchOptions) => void;
  refreshConversationSearchThreads: (query?: string) => Promise<void>;
  selectConversationSearchResult: (result: ThreadSearchResultItem) => void;
  handleConversationSearchKeyDown: (
    event: ReactKeyboardEvent<HTMLInputElement>,
  ) => void;
  setConversationSearchQuery: (query: string) => void;
  clearConversationSearchQuery: () => void;
  setConversationSearchSelectedIndex: (index: number) => void;
} {
  const [conversationSearch, setConversationSearch] =
    useState<ConversationSearchState>(initialConversationSearch);
  const conversationSearchRef = useRef<HTMLDivElement>(null);
  const conversationSearchInputRef = useRef<HTMLInputElement>(null);
  const conversationSearchRequestRef = useRef(0);
  const conversationSearchCloseTimerRef = useRef<number | undefined>(
    undefined,
  );
  const conversationSearchResults = conversationSearch.results;

  useEffect(() => {
    return () => {
      if (conversationSearchCloseTimerRef.current !== undefined) {
        window.clearTimeout(conversationSearchCloseTimerRef.current);
        conversationSearchCloseTimerRef.current = undefined;
      }
    };
  }, []);

  useEffect(() => {
    if (!conversationSearch.open || conversationSearch.closing) {
      return undefined;
    }
    const delay = conversationSearch.query.trim() ? 140 : 0;
    const timer = window.setTimeout(() => {
      void refreshConversationSearchThreads(conversationSearch.query);
    }, delay);
    return () => window.clearTimeout(timer);
  }, [
    conversationSearch.closing,
    conversationSearch.open,
    conversationSearch.query,
  ]);

  function toggleConversationSearch(): void {
    if (conversationSearch.open) {
      closeConversationSearch();
      return;
    }
    openConversationSearch();
  }

  function openConversationSearch(): void {
    if (!activeContext) {
      return;
    }
    if (conversationSearchCloseTimerRef.current !== undefined) {
      window.clearTimeout(conversationSearchCloseTimerRef.current);
      conversationSearchCloseTimerRef.current = undefined;
    }
    onOpen();
    setConversationSearch((current) => ({
      ...current,
      open: true,
      closing: false,
      loading: true,
      error: "",
      selectedIndex: 0,
    }));
    window.requestAnimationFrame(() =>
      conversationSearchInputRef.current?.focus(),
    );
  }

  function closeConversationSearch(
    options: CloseConversationSearchOptions = {},
  ): void {
    if (!conversationSearch.open && !conversationSearch.closing) {
      return;
    }
    conversationSearchRequestRef.current += 1;
    if (conversationSearchCloseTimerRef.current !== undefined) {
      window.clearTimeout(conversationSearchCloseTimerRef.current);
      conversationSearchCloseTimerRef.current = undefined;
    }
    const closeImmediately = options.immediate || prefersReducedMotion();
    setConversationSearch((current) => ({
      ...current,
      open: false,
      closing: !closeImmediately,
      loading: false,
      error: "",
    }));
    if (closeImmediately) {
      return;
    }
    conversationSearchCloseTimerRef.current = window.setTimeout(() => {
      conversationSearchCloseTimerRef.current = undefined;
      setConversationSearch((current) =>
        current.open ? current : { ...current, closing: false },
      );
    }, CONVERSATION_SEARCH_EXIT_MS);
  }

  async function refreshConversationSearchThreads(
    query = conversationSearch.query,
  ): Promise<void> {
    const sourceContext = getAppState().activeContext;
    if (!sourceContext) {
      return;
    }
    const requestID = conversationSearchRequestRef.current + 1;
    conversationSearchRequestRef.current = requestID;
    setConversationSearch((current) => ({
      ...current,
      loading: true,
      error: "",
    }));
    try {
      const search = await window.wuu.searchThreads(
        query,
        CONVERSATION_SEARCH_RESULT_LIMIT,
      );
      if (
        requestID !== conversationSearchRequestRef.current ||
        !sameRuntimeContext(sourceContext, getAppState().activeContext)
      ) {
        return;
      }
      const threads = search.results.map((result) => result.thread);
      setAppState((current) => ({
        ...current,
        threads: mergeListedThreads(current.threads, threads),
      }));
      setConversationSearch((current) => ({
        ...current,
        results: search.results,
        loading: false,
        error: "",
        selectedIndex: Math.max(
          0,
          Math.min(current.selectedIndex, search.results.length - 1),
        ),
      }));
    } catch (error) {
      if (
        requestID !== conversationSearchRequestRef.current ||
        !sameRuntimeContext(sourceContext, getAppState().activeContext)
      ) {
        return;
      }
      setConversationSearch((current) => ({
        ...current,
        loading: false,
        error: error instanceof Error ? error.message : "搜索会话失败",
      }));
    }
  }

  function selectConversationSearchResult(result: ThreadSearchResultItem): void {
    closeConversationSearch();
    onSelectThread(result.thread.id);
  }

  function handleConversationSearchKeyDown(
    event: ReactKeyboardEvent<HTMLInputElement>,
  ): void {
    if (event.key === "Escape") {
      event.preventDefault();
      closeConversationSearch();
      return;
    }
    if (event.key === "ArrowDown" && conversationSearchResults.length > 0) {
      event.preventDefault();
      setConversationSearch((current) => ({
        ...current,
        selectedIndex:
          (current.selectedIndex + 1) % conversationSearchResults.length,
      }));
      return;
    }
    if (event.key === "ArrowUp" && conversationSearchResults.length > 0) {
      event.preventDefault();
      setConversationSearch((current) => ({
        ...current,
        selectedIndex:
          (current.selectedIndex - 1 + conversationSearchResults.length) %
          conversationSearchResults.length,
      }));
      return;
    }
    if (event.metaKey && /^[1-9]$/.test(event.key)) {
      const index = Number(event.key) - 1;
      const result = conversationSearchResults[index];
      if (result) {
        event.preventDefault();
        selectConversationSearchResult(result);
      }
      return;
    }
    const selectedResult =
      conversationSearchResults[
        Math.max(
          0,
          Math.min(
            conversationSearch.selectedIndex,
            conversationSearchResults.length - 1,
          ),
        )
      ];
    if (event.key === "Enter" && selectedResult) {
      event.preventDefault();
      selectConversationSearchResult(selectedResult);
    }
  }

  function setConversationSearchQuery(query: string): void {
    setConversationSearch((current) => ({
      ...current,
      query,
      selectedIndex: 0,
    }));
  }

  function clearConversationSearchQuery(): void {
    setConversationSearchQuery("");
  }

  function setConversationSearchSelectedIndex(index: number): void {
    setConversationSearch((current) => ({
      ...current,
      selectedIndex: index,
    }));
  }

  return {
    conversationSearch,
    conversationSearchResults,
    conversationSearchRef,
    conversationSearchInputRef,
    toggleConversationSearch,
    closeConversationSearch,
    refreshConversationSearchThreads,
    selectConversationSearchResult,
    handleConversationSearchKeyDown,
    setConversationSearchQuery,
    clearConversationSearchQuery,
    setConversationSearchSelectedIndex,
  };
}

function prefersReducedMotion(): boolean {
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}
