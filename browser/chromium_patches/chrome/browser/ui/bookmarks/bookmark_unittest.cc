diff --git a/chrome/browser/ui/bookmarks/bookmark_unittest.cc b/chrome/browser/ui/bookmarks/bookmark_unittest.cc
index d373a9ee6a..912f2ad6fd 100644
--- a/chrome/browser/ui/bookmarks/bookmark_unittest.cc
+++ b/chrome/browser/ui/bookmarks/bookmark_unittest.cc
@@ -15,9 +15,11 @@
 #include "chrome/common/webui_url_constants.h"
 #include "chrome/test/base/browser_with_test_window_test.h"
 #include "components/bookmarks/browser/bookmark_utils.h"
+#include "components/bookmarks/common/bookmark_pref_names.h"
 #include "components/bookmarks/test/bookmark_test_helpers.h"
 #include "components/dom_distiller/core/url_constants.h"
 #include "components/dom_distiller/core/url_utils.h"
+#include "components/prefs/pref_service.h"
 #include "components/saved_tab_groups/public/tab_group_sync_service.h"
 #include "components/saved_tab_groups/test_support/fake_tab_group_sync_service.h"
 #include "components/tabs/public/tab_group.h"
@@ -54,6 +56,20 @@ TEST_F(BookmarkTest, NonEmptyBookmarkBarShownOnNTP) {
             BookmarkBarController::From(browser())->bookmark_bar_state());
 }

+TEST_F(BookmarkTest, BookmarkBarHiddenOnWuuPage) {
+  bookmarks::BookmarkModel* bookmark_model =
+      BookmarkModelFactory::GetForBrowserContext(profile());
+  bookmarks::test::WaitForBookmarkModelToLoad(bookmark_model);
+
+  bookmarks::AddIfNotBookmarked(bookmark_model, GURL("https://www.test.com"),
+                                std::u16string());
+  profile()->GetPrefs()->SetBoolean(bookmarks::prefs::kShowBookmarkBar, true);
+
+  AddTab(browser(), GURL("chrome://wuu"));
+  EXPECT_EQ(BookmarkBar::HIDDEN,
+            BookmarkBarController::From(browser())->bookmark_bar_state());
+}
+
 TEST_F(BookmarkTest, EmptyBookmarkBarNotShownOnNTP) {
   bookmarks::BookmarkModel* bookmark_model =
       BookmarkModelFactory::GetForBrowserContext(profile());
