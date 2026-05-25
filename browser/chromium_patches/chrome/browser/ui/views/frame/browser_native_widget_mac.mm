diff --git a/chrome/browser/ui/views/frame/browser_native_widget_mac.mm b/chrome/browser/ui/views/frame/browser_native_widget_mac.mm
index 1fa6bb2578622..b871faddd3e1a 100644
--- a/chrome/browser/ui/views/frame/browser_native_widget_mac.mm
+++ b/chrome/browser/ui/views/frame/browser_native_widget_mac.mm
@@ -531,6 +531,9 @@ views::Widget::InitParams BrowserNativeWidgetMac::GetWidgetParams(
     views::Widget::InitParams::Ownership ownership) {
   views::Widget::InitParams params(ownership);
   params.native_widget = this;
+  if (browser_view_) {
+    params.headless = browser_view_->browser()->is_hidden();
+  }
   return params;
 }
 
