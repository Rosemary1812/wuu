diff --git a/chrome/browser/ui/views/tabs/vertical/vertical_tab_strip_bottom_container_interactive_uitest.cc b/chrome/browser/ui/views/tabs/vertical/vertical_tab_strip_bottom_container_interactive_uitest.cc
index b2e2db2fd9..8dc629a7c7 100644
--- a/chrome/browser/ui/views/tabs/vertical/vertical_tab_strip_bottom_container_interactive_uitest.cc
+++ b/chrome/browser/ui/views/tabs/vertical/vertical_tab_strip_bottom_container_interactive_uitest.cc
@@ -7,14 +7,12 @@
 #include "base/test/metrics/user_action_tester.h"
 #include "build/build_config.h"
 #include "chrome/browser/ui/browser_element_identifiers.h"
-#include "chrome/browser/ui/views/bookmarks/saved_tab_groups/saved_tab_group_everything_menu.h"
 #include "chrome/browser/ui/views/test/vertical_tabs_interactive_test_mixin.h"
 #include "chrome/test/interaction/interactive_browser_test.h"
 #include "components/prefs/pref_service.h"
 #include "content/public/test/browser_test.h"
 #include "ui/base/interaction/element_identifier.h"
 #include "ui/base/interaction/interaction_test_util.h"
-#include "ui/views/controls/menu/menu_item_view.h"
 
 namespace base::test {
 
@@ -48,22 +46,17 @@ IN_PROC_BROWSER_TEST_F(VerticalTabStripBottomContainerInteractiveUiTest,
       }));
 }
 
-// This test checks that we can click the tab group button in the bottom
-// container of the vertical tab strip
+// Wuu Browser hides the saved tab group button in the vertical tab strip top
+// controls to keep the browser chrome focused on tab navigation.
 IN_PROC_BROWSER_TEST_F(VerticalTabStripBottomContainerInteractiveUiTest,
-                       VerifyTabGroupButton) {
+                       VerifySavedTabGroupButtonHidden) {
   RunTestSequence(
       CheckResult([this]() { return browser()->tab_strip_model()->count(); },
                   1),
       WaitForShow(kVerticalTabStripBottomContainerElementId),
-      EnsurePresent(kSavedTabGroupButtonElementId),
-      PressButton(kSavedTabGroupButtonElementId,
-                  ui::test::InteractionTestUtil::InputType::kDontCare),
-      EnsurePresent(tab_groups::STGEverythingMenu::kCreateNewTabGroup),
-      SelectMenuItem(tab_groups::STGEverythingMenu::kCreateNewTabGroup),
-      WaitForShow(kTabGroupHeaderElementId),
+      EnsureNotPresent(kSavedTabGroupButtonElementId),
       CheckResult([this]() { return browser()->tab_strip_model()->count(); },
-                  2));
+                  1));
 }
 
 }  // namespace base::test
