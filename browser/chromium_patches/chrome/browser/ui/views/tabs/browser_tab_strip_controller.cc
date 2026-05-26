diff --git a/chrome/browser/ui/views/tabs/browser_tab_strip_controller.cc b/chrome/browser/ui/views/tabs/browser_tab_strip_controller.cc
index 9fd32505f1..3f84aca2f8 100644
--- a/chrome/browser/ui/views/tabs/browser_tab_strip_controller.cc
+++ b/chrome/browser/ui/views/tabs/browser_tab_strip_controller.cc
@@ -896,8 +896,10 @@ const BrowserFrameView* BrowserTabStripController::GetFrameView() const {
 }

 void BrowserTabStripController::SetTabDataAt(int model_index) {
-  tabstrip_->SetTabData(model_index, TabRendererData::FromTabInterface(
-                                         model_->GetTabAtIndex(model_index)));
+  TabRendererData data =
+      TabRendererData::FromTabInterface(model_->GetTabAtIndex(model_index));
+  data.can_close = web_app::IsTabClosable(model_, model_index);
+  tabstrip_->SetTabData(model_index, std::move(data));
 }

 void BrowserTabStripController::AddTabs(
@@ -907,9 +909,11 @@ void BrowserTabStripController::AddTabs(

   std::vector<TabStrip::AddTabData> tabs_data;
   for (const auto& [tab, index] : contents_list) {
+    TabRendererData data = TabRendererData::FromTabInterface(tab);
+    data.can_close = web_app::IsTabClosable(model_, index);
     tabs_data.push_back({.index = index,
                          .handle = tab->GetHandle(),
-                         .data = TabRendererData::FromTabInterface(tab)});
+                         .data = std::move(data)});
   }

   tabstrip_->AddTabsAt(std::move(tabs_data));
