diff --git a/ui/views/cocoa/native_widget_mac_ns_window_host.mm b/ui/views/cocoa/native_widget_mac_ns_window_host.mm
index e40d0d5be1c48..0f4940de7214b 100644
--- a/ui/views/cocoa/native_widget_mac_ns_window_host.mm
+++ b/ui/views/cocoa/native_widget_mac_ns_window_host.mm
@@ -475,6 +475,7 @@ void NativeWidgetMacNSWindowHost::InitWindow(
     window_params->is_translucent =
         params.opacity == Widget::InitParams::WindowOpacity::kTranslucent;
     window_params->is_tooltip = is_tooltip;
+    window_params->is_headless = params.headless;
 
     // macOS likes to put shadows on most things. However, frameless windows
     // (with styleMask = NSWindowStyleMaskBorderless) default to no shadow. So
