diff --git a/chrome/browser/ui/browser_commands.cc b/chrome/browser/ui/browser_commands.cc
index 3f477375e0..32a2056539 100644
--- a/chrome/browser/ui/browser_commands.cc
+++ b/chrome/browser/ui/browser_commands.cc
@@ -27,6 +27,7 @@
 #include "build/build_config.h"
 #include "chrome/app/chrome_command_ids.h"
 #include "chrome/browser/bookmarks/bookmark_model_factory.h"
+#include "chrome/browser/browseros/core/browseros_constants.h"
 #include "chrome/browser/browser_process.h"
 #include "chrome/browser/browsing_data/chrome_browsing_data_remover_delegate.h"
 #include "chrome/browser/chained_back_navigation_tracker.h"
@@ -750,7 +751,10 @@ Browser* OpenEmptyWindow(Profile* profile,
   // Startup tabs could be created during browser creation. Add an empty tab
   // only if no tabs are created.
   if (browser->tab_strip_model()->empty()) {
-    AddTabAt(browser, GURL(), -1, true);
+    const GURL initial_url = profile->IsOffTheRecord()
+                                 ? GURL()
+                                 : GURL(browseros::kWuuBrowserURL);
+    AddTabAt(browser, initial_url, -1, true);
   }

   browser->window()->Show();
@@ -2474,7 +2478,20 @@ bool IsDebuggerAttachedToCurrentTab(Browser* browser) {

 void CopyURL(BrowserWindowInterface* bwi, content::WebContents* web_contents) {
   ui::ScopedClipboardWriter scw(ui::ClipboardBuffer::kCopyPaste);
-  scw.WriteText(base::UTF8ToUTF16(web_contents->GetVisibleURL().spec()));
+  GURL url = web_contents->GetVisibleURL();
+
+  // Transform BrowserOS extension URLs to virtual URLs for copying
+  if (url.SchemeIs(extensions::kExtensionScheme)) {
+    std::string virtual_url = browseros::GetBrowserOSVirtualURL(
+        url.host(), url.path(), url.ref());
+    if (!virtual_url.empty()) {
+      scw.WriteText(base::UTF8ToUTF16(virtual_url));
+    } else {
+      scw.WriteText(base::UTF8ToUTF16(url.spec()));
+    }
+  } else {
+    scw.WriteText(base::UTF8ToUTF16(url.spec()));
+  }

 #if !BUILDFLAG(IS_ANDROID)
   if (toast_features::IsEnabled(toast_features::kLinkCopiedToast)) {
