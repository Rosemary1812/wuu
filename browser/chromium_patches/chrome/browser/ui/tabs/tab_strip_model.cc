diff --git a/chrome/browser/ui/tabs/tab_strip_model.cc b/chrome/browser/ui/tabs/tab_strip_model.cc
index 15d72c6972..13b3673b5d 100644
--- a/chrome/browser/ui/tabs/tab_strip_model.cc
+++ b/chrome/browser/ui/tabs/tab_strip_model.cc
@@ -39,6 +39,7 @@
 #include "base/types/pass_key.h"
 #include "build/build_config.h"
 #include "chrome/app/chrome_command_ids.h"
+#include "chrome/browser/browseros/core/browseros_constants.h"
 #include "chrome/browser/commerce/browser_utils.h"
 #include "chrome/browser/content_settings/host_content_settings_map_factory.h"
 #include "chrome/browser/extensions/tab_helper.h"
@@ -126,6 +127,27 @@ namespace {

 TabGroupModelFactory* factory_instance = nullptr;

+bool IsWuuWorkbenchURL(const GURL& url) {
+  if (url.SchemeIs(content::kChromeUIScheme)) {
+    return browseros::IsWuuBrowserProductHost(url.host());
+  }
+
+  return url.SchemeIs("chrome-extension") &&
+         url.host() == browseros::kAgentExtensionId &&
+         url.path() == "/app.html";
+}
+
+bool IsProtectedWuuWorkbenchTab(const TabStripModel* tab_strip_model,
+                                int index) {
+  if (!tab_strip_model->ContainsIndex(index) || index != 0) {
+    return false;
+  }
+
+  content::WebContents* contents = tab_strip_model->GetWebContentsAt(index);
+  return IsWuuWorkbenchURL(contents->GetVisibleURL()) ||
+         IsWuuWorkbenchURL(contents->GetLastCommittedURL());
+}
+
 // Works similarly to base::AutoReset but checks for access from the wrong
 // thread as well as ensuring that the previous value of the re-entrancy guard
 // variable was false.
@@ -1461,6 +1483,10 @@ bool TabStripModel::IsTabInForeground(int index) const {
 }

 bool TabStripModel::IsTabClosable(int index) const {
+  if (IsProtectedWuuWorkbenchTab(this, index) && !closing_all_) {
+    return false;
+  }
+
   return PolicyAllowsTabClosing(GetWebContentsAt(index));
 }
