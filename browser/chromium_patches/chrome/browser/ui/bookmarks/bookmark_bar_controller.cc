diff --git a/chrome/browser/ui/bookmarks/bookmark_bar_controller.cc b/chrome/browser/ui/bookmarks/bookmark_bar_controller.cc
index fde20bc312..b28061eaf6 100644
--- a/chrome/browser/ui/bookmarks/bookmark_bar_controller.cc
+++ b/chrome/browser/ui/bookmarks/bookmark_bar_controller.cc
@@ -6,6 +6,7 @@

 #include "chrome/browser/bookmarks/bookmark_model_factory.h"
 #include "chrome/browser/browser_features.h"
+#include "chrome/browser/browseros/core/browseros_constants.h"
 #include "chrome/browser/defaults.h"
 #include "chrome/browser/profiles/profile.h"
 #include "chrome/browser/search/search.h"
@@ -31,6 +32,7 @@
 #include "content/public/browser/navigation_entry.h"
 #include "content/public/browser/navigation_handle.h"
 #include "content/public/browser/web_contents.h"
+#include "content/public/common/url_constants.h"

 namespace {

@@ -56,6 +58,21 @@ bool IsShowingNTP(content::WebContents* web_contents) {
          search::NavEntryIsInstantNTP(web_contents, entry);
 }

+bool IsShowingWuuProductPage(content::WebContents* web_contents) {
+  if (SadTab::ShouldShow(web_contents->GetCrashedStatus())) {
+    return false;
+  }
+
+  content::NavigationEntry* entry =
+      web_contents->GetController().GetLastCommittedEntry();
+  if (entry->IsInitialEntry()) {
+    entry = web_contents->GetController().GetVisibleEntry();
+  }
+  const GURL& url = entry->GetURL();
+  return url.SchemeIs(content::kChromeUIScheme) &&
+         browseros::IsWuuBrowserProductHost(url.host());
+}
+
 }  // namespace

 DEFINE_USER_DATA(BookmarkBarController);
@@ -144,6 +161,14 @@ bool BookmarkBarController::ShouldShowBookmarkBar() const {
     return false;
   }

+  std::vector<tabs::TabInterface*> tabs = tab_strip_model_->GetForegroundTabs();
+  if (std::any_of(tabs.begin(), tabs.end(), [](const tabs::TabInterface* tab) {
+        return tab->GetContents() &&
+               IsShowingWuuProductPage(tab->GetContents());
+      })) {
+    return false;
+  }
+
   if (browser_defaults::bookmarks_enabled &&
       profile->GetPrefs()->GetBoolean(bookmarks::prefs::kShowBookmarkBar) &&
       !browser_->ShouldHideUIForFullscreen()) {
@@ -164,7 +189,6 @@ bool BookmarkBarController::ShouldShowBookmarkBar() const {
     return false;
   }

-  std::vector<tabs::TabInterface*> tabs = tab_strip_model_->GetForegroundTabs();
   if (tabs.empty()) {
     return false;
   }
