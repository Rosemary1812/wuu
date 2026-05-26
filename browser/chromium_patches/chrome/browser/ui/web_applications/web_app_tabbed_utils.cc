diff --git a/chrome/browser/ui/web_applications/web_app_tabbed_utils.cc b/chrome/browser/ui/web_applications/web_app_tabbed_utils.cc
index d6359ee19d..3ae7e50348 100644
--- a/chrome/browser/ui/web_applications/web_app_tabbed_utils.cc
+++ b/chrome/browser/ui/web_applications/web_app_tabbed_utils.cc
@@ -4,10 +4,38 @@

 #include "chrome/browser/ui/web_applications/web_app_tabbed_utils.h"

+#include "chrome/browser/browseros/core/browseros_constants.h"
 #include "chrome/browser/ui/browser.h"
 #include "chrome/browser/ui/tabs/tab_strip_model.h"
 #include "chrome/browser/ui/tabs/tab_strip_model_delegate.h"
 #include "chrome/browser/ui/web_applications/app_browser_controller.h"
+#include "content/public/browser/web_contents.h"
+#include "content/public/common/url_constants.h"
+
+namespace {
+
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
+}  // namespace

 namespace web_app {

@@ -24,6 +52,10 @@ bool IsPinnedHomeTab(const TabStripModel* tab_strip_model, int index) {
 }

 bool IsTabClosable(const TabStripModel* tab_strip_model, int index) {
+  if (IsProtectedWuuWorkbenchTab(tab_strip_model, index)) {
+    return tab_strip_model->closing_all();
+  }
+
   return !IsPinnedHomeTab(tab_strip_model, index) ||
          tab_strip_model->count() == 1;
 }
