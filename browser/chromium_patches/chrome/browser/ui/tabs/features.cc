diff --git a/chrome/browser/ui/tabs/features.cc b/chrome/browser/ui/tabs/features.cc
index 27ef74af63..daa403a55a 100644
--- a/chrome/browser/ui/tabs/features.cc
+++ b/chrome/browser/ui/tabs/features.cc
@@ -11,7 +11,7 @@ namespace tabs {
 
 BASE_FEATURE(kTabGroupHome, base::FEATURE_DISABLED_BY_DEFAULT);
 
-BASE_FEATURE(kVerticalTabs, base::FEATURE_DISABLED_BY_DEFAULT);
+BASE_FEATURE(kVerticalTabs, base::FEATURE_ENABLED_BY_DEFAULT);
 
 BASE_FEATURE(kVerticalTabsPreviewBadge, base::FEATURE_DISABLED_BY_DEFAULT);
 
