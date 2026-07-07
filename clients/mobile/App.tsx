// App shell: three screens (pair → chats → thread) with hand-rolled
// navigation — the surface is small enough that a navigation library would
// outweigh the app. The controller is a module singleton; screens read the
// store via useSyncExternalStore.

import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { ActivityIndicator, Alert, BackHandler, StyleSheet, View } from "react-native";
import { StatusBar } from "expo-status-bar";

import { WuuMobile } from "./src/lib/connection";
import { deviceCredentialStore } from "./src/lib/credStore";
import { threadDisplayTitle } from "./src/lib/threads";
import { usePalette } from "./src/theme";
import { PairScreen } from "./src/screens/PairScreen";
import { ChatsScreen } from "./src/screens/ChatsScreen";
import { ThreadScreen } from "./src/screens/ThreadScreen";

const controller = new WuuMobile(deviceCredentialStore);

type Route =
  | { name: "boot" }
  | { name: "pair" }
  | { name: "chats" }
  | { name: "thread"; threadId: string };

export default function App(): React.JSX.Element {
  const palette = usePalette();
  const [route, setRoute] = useState<Route>({ name: "boot" });
  const [refreshing, setRefreshing] = useState(false);
  const snapshot = useSyncExternalStore(controller.store.subscribe, controller.store.getSnapshot);

  useEffect(() => {
    void controller
      .startFromStoredCredentials()
      .then((hadCredentials) => setRoute(hadCredentials ? { name: "chats" } : { name: "pair" }))
      .catch(() => setRoute({ name: "pair" }));
  }, []);

  // Android hardware back mirrors the header back button.
  useEffect(() => {
    const sub = BackHandler.addEventListener("hardwareBackPress", () => {
      if (route.name === "thread") {
        controller.closeThread();
        setRoute({ name: "chats" });
        return true;
      }
      return false;
    });
    return () => sub.remove();
  }, [route.name]);

  const openThread = useCallback((threadId: string) => {
    setRoute({ name: "thread", threadId });
    void controller.openThread(threadId).catch(() => {});
  }, []);

  const refresh = useCallback(() => {
    setRefreshing(true);
    void controller
      .refreshThreads()
      .catch(() => {})
      .finally(() => setRefreshing(false));
  }, []);

  return (
    <View style={[styles.root, { backgroundColor: palette.paper }]}>
      <StatusBar style="auto" />
      {route.name === "boot" ? (
        <View style={styles.boot}>
          <ActivityIndicator color={palette.accent} />
        </View>
      ) : route.name === "pair" ? (
        <PairScreen
          onPair={async (uri) => {
            const creds = await controller.pairWithUri(uri, "手机");
            return creds.host_name ?? "";
          }}
          onDone={() => setRoute({ name: "chats" })}
        />
      ) : route.name === "chats" ? (
        <ChatsScreen
          snapshot={snapshot}
          refreshing={refreshing}
          onRefresh={refresh}
          onOpenThread={(thread) => openThread(thread.id)}
          onLongPressThread={(thread) => {
            Alert.alert(threadDisplayTitle(thread), undefined, [
              {
                text: thread.pinned ? "取消置顶" : "置顶",
                onPress: () => void controller.togglePin(thread).catch(() => {}),
              },
              { text: "取消", style: "cancel" },
            ]);
          }}
        />
      ) : (
        <ThreadScreen
          snapshot={snapshot}
          threadId={route.threadId}
          onBack={() => {
            controller.closeThread();
            setRoute({ name: "chats" });
          }}
          onSend={(thread, text) => controller.sendMessage(thread, text)}
          onInterrupt={(threadId) => void controller.interrupt(threadId).catch(() => {})}
          onReact={(threadId, seq, reaction) =>
            void controller.react(threadId, seq, reaction).catch(() => {})
          }
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  boot: { flex: 1, alignItems: "center", justifyContent: "center" },
});
