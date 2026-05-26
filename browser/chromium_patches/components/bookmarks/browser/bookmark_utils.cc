diff --git a/components/bookmarks/browser/bookmark_utils.cc b/components/bookmarks/browser/bookmark_utils.cc
index d5beb2ca11..221fac0ea4 100644
--- a/components/bookmarks/browser/bookmark_utils.cc
+++ b/components/bookmarks/browser/bookmark_utils.cc
@@ -457,7 +457,7 @@ void RegisterProfilePrefs(user_prefs::PrefRegistrySyncable* registry) {
       prefs::kShowAppsShortcutInBookmarkBar, false,
       user_prefs::PrefRegistrySyncable::SYNCABLE_PREF);
   registry->RegisterBooleanPref(
-      prefs::kShowTabGroupsInBookmarkBar, true,
+      prefs::kShowTabGroupsInBookmarkBar, false,
       user_prefs::PrefRegistrySyncable::SYNCABLE_PREF);
   registry->RegisterBooleanPref(
       prefs::kShowManagedBookmarksInBookmarkBar, true,
