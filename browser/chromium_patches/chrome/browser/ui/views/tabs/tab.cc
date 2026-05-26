diff --git a/chrome/browser/ui/views/tabs/tab.cc b/chrome/browser/ui/views/tabs/tab.cc
index e06a60bba0..654f00d205 100644
--- a/chrome/browser/ui/views/tabs/tab.cc
+++ b/chrome/browser/ui/views/tabs/tab.cc
@@ -1172,19 +1172,17 @@ void Tab::UpdateIconVisibility() {

 #if BUILDFLAG(IS_CHROMEOS)
   const bool should_show_close_button =
+      data_.can_close &&
       !IsLockedForOnTask(controller_->GetBrowserWindowInterface());
+#else
+  const bool should_show_close_button = data_.can_close;
 #endif  // BUILDFLAG(IS_CHROMEOS)

   if (IsActive()) {
-#if BUILDFLAG(IS_CHROMEOS)
-    // Hide tab close button for OnTask if locked. Only applicable for non-web
-    // browser scenarios.
     showing_close_button_ = should_show_close_button;
-#else
-    // Close button is shown on active tabs regardless of the size.
-    showing_close_button_ = true;
-#endif  // BUILDFLAG(IS_CHROMEOS)
-    available_width -= close_button_width;
+    if (showing_close_button_) {
+      available_width -= close_button_width;
+    }

     showing_alert_indicator_ =
         has_alert_icon && alert_icon_width <= available_width;
@@ -1209,10 +1207,7 @@ void Tab::UpdateIconVisibility() {
     }

     showing_close_button_ =
-#if BUILDFLAG(IS_CHROMEOS)
-        should_show_close_button &&
-#endif
-        large_enough_for_close_button;
+        should_show_close_button && large_enough_for_close_button;
     if (showing_close_button_) {
       available_width -= close_button_width;
     }
