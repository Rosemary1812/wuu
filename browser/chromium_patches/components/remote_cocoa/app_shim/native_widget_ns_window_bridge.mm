diff --git a/components/remote_cocoa/app_shim/native_widget_ns_window_bridge.mm b/components/remote_cocoa/app_shim/native_widget_ns_window_bridge.mm
index b89085a51a..13314d0803 100644
--- a/components/remote_cocoa/app_shim/native_widget_ns_window_bridge.mm
+++ b/components/remote_cocoa/app_shim/native_widget_ns_window_bridge.mm
@@ -531,7 +531,7 @@ NSUInteger CountBridgedWindows(NSArray* child_windows) {
   is_translucent_window_ = params->is_translucent;
   pending_restoration_data_ = params->state_restoration_data;

-  if (display::Screen::Get()->IsHeadless()) {
+  if (params->is_headless || display::Screen::Get()->IsHeadless()) {
     [window_ setIsHeadless:YES];
   }

