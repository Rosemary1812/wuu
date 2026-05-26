diff --git a/chrome/browser/ui/tabs/tab_renderer_data.cc b/chrome/browser/ui/tabs/tab_renderer_data.cc
index f0c36b64a9..0e6838ea5d 100644
--- a/chrome/browser/ui/tabs/tab_renderer_data.cc
+++ b/chrome/browser/ui/tabs/tab_renderer_data.cc
@@ -194,7 +194,8 @@ bool TabRendererData::operator==(const TabRendererData& other) const {
          last_committed_url == other.last_committed_url &&
          should_display_url == other.should_display_url &&
          is_crashed == other.is_crashed && show_icon == other.show_icon &&
-         pinned == other.pinned && blocked == other.blocked &&
+         pinned == other.pinned && can_close == other.can_close &&
+         blocked == other.blocked &&
          alert_state == other.alert_state &&
          should_hide_throbber == other.should_hide_throbber &&
          is_tab_discarded == other.is_tab_discarded &&
