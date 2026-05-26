diff --git a/chrome/browser/ui/toolbar/toolbar_pref_names.cc b/chrome/browser/ui/toolbar/toolbar_pref_names.cc
index b0e7b69174614..828ce4ced20ec 100644
--- a/chrome/browser/ui/toolbar/toolbar_pref_names.cc
+++ b/chrome/browser/ui/toolbar/toolbar_pref_names.cc
@@ -14,14 +14,7 @@ namespace toolbar {
 
 void RegisterProfilePrefs(user_prefs::PrefRegistrySyncable* registry) {
   base::ListValue default_pinned_actions;
-  const std::optional<std::string>& chrome_labs_action =
-      actions::ActionIdMap::ActionIdToString(kActionShowChromeLabs);
-  // ActionIdToStringMappings are not initialized in unit tests, therefore will
-  // not have a value. In the normal case, the action should always have a
-  // value.
-  if (chrome_labs_action.has_value()) {
-    default_pinned_actions.Append(chrome_labs_action.value());
-  }
+  // Chrome Labs is no longer pinned by default
 
   if (features::HasTabSearchToolbarButton()) {
     const std::optional<std::string>& tab_search_action =
@@ -46,6 +39,12 @@ void RegisterProfilePrefs(user_prefs::PrefRegistrySyncable* registry) {
   registry->RegisterBooleanPref(
       prefs::kTabSearchMigrationComplete, false,
       user_prefs::PrefRegistrySyncable::SYNCABLE_PREF);
+  registry->RegisterBooleanPref(
+      prefs::kPinnedThirdPartyLlmMigrationComplete, false,
+      user_prefs::PrefRegistrySyncable::SYNCABLE_PREF);
+  registry->RegisterBooleanPref(
+      prefs::kPinnedClashOfGptsMigrationComplete, false,
+      user_prefs::PrefRegistrySyncable::SYNCABLE_PREF);
 }
 
 }  // namespace toolbar
