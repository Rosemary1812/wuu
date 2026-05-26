diff --git a/chrome/browser/ui/tabs/tab_renderer_data.h b/chrome/browser/ui/tabs/tab_renderer_data.h
index 05fdcb75bb..d68cd8db20 100644
--- a/chrome/browser/ui/tabs/tab_renderer_data.h
+++ b/chrome/browser/ui/tabs/tab_renderer_data.h
@@ -54,6 +54,7 @@ struct TabRendererData {
   bool is_crashed = false;
   bool show_icon = true;
   bool pinned = false;
+  bool can_close = true;
   bool blocked = false;
   std::vector<tabs::TabAlert> alert_state;
   bool should_hide_throbber = false;
