diff --git a/chrome/browser/ui/views/tabs/vertical/vertical_tab_strip_top_container.cc b/chrome/browser/ui/views/tabs/vertical/vertical_tab_strip_top_container.cc
index 24586bfc4e..f54d86a539 100644
--- a/chrome/browser/ui/views/tabs/vertical/vertical_tab_strip_top_container.cc
+++ b/chrome/browser/ui/views/tabs/vertical/vertical_tab_strip_top_container.cc
@@ -58,21 +58,6 @@ VerticalTabStripTopContainer::VerticalTabStripTopContainer(
     tab_group_button = CreateFlatEdgeButtonFor(kActionToggleProjectsPanel);
     tab_group_button->SetProperty(views::kElementIdentifierKey,
                                   kVerticalTabStripProjectsButtonElementId);
-  } else if (tab_groups::SavedTabGroupUtils::IsEnabledForProfile(
-                 browser_->GetProfile())) {
-    tab_group_button = CreateFlatEdgeButtonFor(kActionTabGroupsMenu);
-    // Creating MenuButtonController because tab_group_button is a LabelButton.
-    auto controller = std::make_unique<views::MenuButtonController>(
-        tab_group_button.get(),
-        base::BindRepeating(&VerticalTabStripTopContainer::ShowEverythingMenu,
-                            base::Unretained(this)),
-        std::make_unique<views::Button::DefaultButtonControllerDelegate>(
-            tab_group_button.get()));
-    everything_menu_controller_ = controller.get();
-
-    tab_group_button->SetButtonController(std::move(controller));
-    tab_group_button->SetProperty(views::kElementIdentifierKey,
-                                  kSavedTabGroupButtonElementId);
   }
 
   auto tab_search_button = CreateFlatEdgeButtonFor(kActionTabSearch);
