diff --git a/chrome/browser/ui/views/frame/browser_view.cc b/chrome/browser/ui/views/frame/browser_view.cc
index a29e3f73ef..7c3e6372c9 100644
--- a/chrome/browser/ui/views/frame/browser_view.cc
+++ b/chrome/browser/ui/views/frame/browser_view.cc
@@ -1277,9 +1277,9 @@ bool BrowserView::UsesImmersiveFullscreenMode() const {
 }
 
 bool BrowserView::UsesImmersiveFullscreenTabbedMode() const {
-  return (GetSupportsTabStrip() &&
-          base::FeatureList::IsEnabled(features::kImmersiveFullscreen)) &&
-         !GetIsWebAppType();
+  // Wuu keeps horizontal tabs in the top-container stack during fullscreen so
+  // the visual order matches the normal browser layout: tabs above the toolbar.
+  return false;
 }
 #endif
 
