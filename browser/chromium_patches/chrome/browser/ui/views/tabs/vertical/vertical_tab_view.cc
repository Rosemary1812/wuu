diff --git a/chrome/browser/ui/views/tabs/vertical/vertical_tab_view.cc b/chrome/browser/ui/views/tabs/vertical/vertical_tab_view.cc
index df58bd0e81..d56650594b 100644
--- a/chrome/browser/ui/views/tabs/vertical/vertical_tab_view.cc
+++ b/chrome/browser/ui/views/tabs/vertical/vertical_tab_view.cc
@@ -511,7 +511,7 @@ VerticalTabView::CalculateChildVisibilities() const {
   child_visibility_map[icon_] =
       !pinned_ || !child_visibility_map[alert_indicator_];

-  if (pinned_) {
+  if (!tab_data_.can_close || pinned_) {
     child_visibility_map[close_button_] = false;
   } else if (active_) {
     child_visibility_map[close_button_] = true;
